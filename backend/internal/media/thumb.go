package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"fanke/backend/internal/pluginrpc"
	"image"
	_ "image/gif" // 源图尺寸探测(仅解头部)
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

// 缩略图/转码服务:调 cwebp(libwebp) 把源图缩到目标宽度并转 WebP,结果落盘缓存。
//
// 设计取舍:
//   - 只缩不放:源图比目标窄时按原尺寸编码(PNG→WebP 本身就能省体积),避免糊图;
//   - 缓存键 = 版本 + 源标识 + 宽度。源对象本身不可变(S3 key 含 sha16,uploads 亦然),
//     故缓存永不需要失效,可直接配 immutable 长缓存;
//   - 同一键并发只跑一次(单飞),其余请求等它写完直接读缓存;
//   - 先写 .part 再 rename,保证任何时刻读到的都是完整文件。
const (
	// ThumbVersion 编码参数版本:调整质量/算法时递增即可让整批缓存失效。
	ThumbVersion = "v2"
	// ThumbQuality cwebp 有损质量(-q)。截图/文字类图片在 82 下会明显发虚。
	ThumbQuality = 88
	// thumbMaxSrcBytes 单张源图字节上限(超出直接拒绝,防打爆内存/磁盘)。
	thumbMaxSrcBytes = 100 << 20
)

// thumbableExt 允许转码的源图扩展名(cwebp 可解码的位图)。
// gif 不转(会丢动画),svg 不转(cwebp 不支持),无扩展名不转。
var thumbableExt = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".webp": true,
	".bmp": true, ".tif": true, ".tiff": true,
}

// Thumbable 判断该地址是否适合走缩图(按扩展名;data:/svg/gif 一律不转)。
func Thumbable(raw string) bool {
	if raw == "" || strings.HasPrefix(raw, "data:") {
		return false
	}
	p := raw
	if u, err := url.Parse(raw); err == nil && u.Path != "" {
		p = u.Path
	}
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		p = p[:i]
	}
	return thumbableExt[strings.ToLower(path.Ext(p))]
}

type Thumbnailer struct {
	dir      string
	mu       sync.Mutex
	inflight map[string]chan struct{}
}

func NewThumbnailer(dir string) (*Thumbnailer, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("缩略图缓存目录不可写: %w", err)
	}
	return &Thumbnailer{dir: dir, inflight: map[string]chan struct{}{}}, nil
}

// Dir 缓存目录。
func (t *Thumbnailer) Dir() string { return t.dir }

func (t *Thumbnailer) Purge(srcID string) error {
	sum := sha256.Sum256([]byte(ThumbVersion + "|" + srcID))
	files, err := filepath.Glob(filepath.Join(t.dir, hex.EncodeToString(sum[:20])+"-*.webp"))
	if err != nil {
		return err
	}
	for _, file := range files {
		if err := os.Remove(file); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func (t *Thumbnailer) key(srcID string, w, quality int) string {
	sum := sha256.Sum256([]byte(ThumbVersion + "|" + srcID + "|" + strconv.Itoa(w) + "|" + strconv.Itoa(quality)))
	source := sha256.Sum256([]byte(ThumbVersion + "|" + srcID))
	return hex.EncodeToString(source[:20]) + "-" + hex.EncodeToString(sum[:20])
}

// Path 某源在指定宽度下的缓存文件路径。
func (t *Thumbnailer) Path(srcID string, w int) string {
	return t.PathWithQuality(srcID, w, ThumbQuality)
}

func (t *Thumbnailer) PathWithQuality(srcID string, w, quality int) string {
	return filepath.Join(t.dir, t.key(srcID, w, quality)+".webp")
}

// Cached 是否已有现成的缩略图(外部源据此决定"先回原图、后台慢慢转")。
func (t *Thumbnailer) Cached(srcID string, w int) bool {
	return t.CachedWithQuality(srcID, w, ThumbQuality)
}

func (t *Thumbnailer) CachedWithQuality(srcID string, w, quality int) bool {
	return fileReady(t.PathWithQuality(srcID, w, quality))
}

// Get 返回缩略图文件路径,未命中则调 open 取源图并生成。
func (t *Thumbnailer) Get(ctx context.Context, srcID string, w int, open func(context.Context) (io.ReadCloser, error)) (string, error) {
	return t.GetWithQuality(ctx, srcID, w, ThumbQuality, open)
}

func (t *Thumbnailer) GetWithQuality(ctx context.Context, srcID string, w, quality int, open func(context.Context) (io.ReadCloser, error)) (string, error) {
	final := t.PathWithQuality(srcID, w, quality)
	if fileReady(final) {
		return final, nil
	}
	key := t.key(srcID, w, quality)
	t.mu.Lock()
	if ch, ok := t.inflight[key]; ok {
		t.mu.Unlock()
		// 等领头请求写完,但必须带上 ctx:领头的那个可能卡在 image.process 子进程上,
		// 裸等会把同 key 的后续请求全部无限期挂住,客户端早就断开了也放不掉。
		select {
		case <-ch:
		case <-ctx.Done():
			return "", ctx.Err()
		}
		if fileReady(final) {
			return final, nil
		}
		return "", fmt.Errorf("缩略图生成失败: %s", srcID)
	}
	ch := make(chan struct{})
	t.inflight[key] = ch
	t.mu.Unlock()

	err := t.render(ctx, srcID, w, quality, open, final)

	t.mu.Lock()
	delete(t.inflight, key)
	t.mu.Unlock()
	close(ch)
	if err != nil {
		return "", err
	}
	return final, nil
}

func (t *Thumbnailer) render(ctx context.Context, srcID string, w, quality int, open func(context.Context) (io.ReadCloser, error), final string) error {
	rc, err := open(ctx)
	if err != nil {
		return err
	}
	defer rc.Close()
	tmpIn, err := os.CreateTemp(t.dir, "src-*.part")
	if err != nil {
		return err
	}
	inName := tmpIn.Name()
	defer os.Remove(inName)
	n, err := io.Copy(tmpIn, io.LimitReader(rc, thumbMaxSrcBytes+1))
	if cerr := tmpIn.Close(); err == nil && cerr != nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if n > thumbMaxSrcBytes {
		return fmt.Errorf("源图过大: %d 字节", n)
	}
	if n == 0 {
		return fmt.Errorf("源图内容为空")
	}

	source, err := os.ReadFile(inName)
	if err != nil {
		return err
	}
	if _, err := StaticImageConfig(source); err != nil {
		return err
	}
	tmpOut := final + ".part"
	var result struct {
		Path string `json:"path"`
	}
	runner := pluginrpc.Runner{Root: pluginRoot(), Entrypoint: pluginEntrypoint()}
	if err := runner.Call(ctx, "image.process", map[string]any{"input": inName, "output": tmpOut, "width": w, "quality": quality}, &result); err != nil {
		os.Remove(tmpOut)
		return fmt.Errorf("图片插件失败: %w", err)
	}
	if err := os.Rename(tmpOut, final); err != nil {
		os.Remove(tmpOut)
		return err
	}
	return nil
}

func fileReady(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Size() > 0
}

func pluginRoot() string {
	if v := os.Getenv("FAKE_IMAGE_PLUGIN_DIR"); v != "" {
		return v
	}
	return pluginRootDir("image-processing")
}
func pluginEntrypoint() string {
	if v := os.Getenv("FAKE_IMAGE_PLUGIN_ENTRYPOINT"); v != "" {
		return v
	}
	entry := filepath.Join("server", "bin", runtime.GOOS+"-"+runtime.GOARCH, "image-processing-server")
	if runtime.GOOS == "windows" {
		entry += ".exe"
	}
	return entry
}

// decodeWidth 只解图片头部拿原始宽度;无法识别(webp 等)返回 0(此时不缩放,由 cwebp 按原尺寸编码)。
func decodeWidth(file string) (int, int) {
	f, err := os.Open(file)
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return 0, 0
	}
	return cfg.Width, cfg.Height
}
