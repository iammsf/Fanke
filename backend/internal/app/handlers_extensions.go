package app

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"fanke/backend/internal/pluginrpc"
	"fanke/backend/internal/repo"
)

const (
	// 插件包可能内含编译好的 Go 子进程(如 s3-storage 约 9MB),故上限按二进制体积放宽;
	// 仍然限制文件数与解压总量,且符号链接/路径穿越一律拒绝。
	maxExtPackage = 30 << 20 // 上传包上限 30MB
	maxExtFile    = 12 << 20 // 单文件上限 12MB
	maxExtTotal   = 60 << 20 // 解压后总量上限 60MB
)

// extInfo 插件包 info.json 结构。request 字段声明所需 hook。
type extInfo struct {
	Name        string         `json:"name"`
	Slug        string         `json:"slug"` // 可选:显式指定 slug(中文名必须);缺省由 name 派生
	Author      string         `json:"author"`
	Version     string         `json:"version"`
	Date        string         `json:"date"`
	Request     []string       `json:"request"`
	Description string         `json:"description"`
	URL         string         `json:"url"`
	UpdateURL   string         `json:"update_url"`
	Favico      string         `json:"favico"`
	Extra       map[string]any `json:"-"` // 其余自定义字段原样保留(详情页展示用)
}

var extExplicitSlugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,47}$`)

var extSlugRe = regexp.MustCompile(`[^a-z0-9-]+`)

// extSlug 由 name 派生安全 slug:小写、仅 a-z0-9-。
func extSlug(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = strings.ReplaceAll(s, " ", "-")
	s = extSlugRe.ReplaceAllString(s, "")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "ext-" + strconv.FormatInt(time.Now().Unix(), 36)
	}
	if len(s) > 48 {
		s = s[:48]
	}
	return s
}

// extDir 插件文件落盘目录(每个 slug 一个子目录)。
func (a *App) extDir(slug string) string {
	return filepath.Join(a.Cfg.ExtensionsDir, slug)
}

func (a *App) ensureExtRoot() error {
	return os.MkdirAll(a.Cfg.ExtensionsDir, 0o755)
}

// ---------- 上传/安装 ----------

type extFileEntry struct {
	Name string
	Data []byte
}

// readZip 读取 zip 包内全部文件(限大小/数量,防 zip 炸弹)。
func readZip(data []byte) ([]extFileEntry, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, errors.New("zip 解析失败")
	}
	if len(zr.File) > 200 {
		return nil, errors.New("包内文件过多(>200)")
	}
	out := []extFileEntry{}
	var total int64
	for _, f := range zr.File {
		if f.FileInfo().Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("zip 内不允许符号链接")
		}
		if f.FileInfo().IsDir() {
			continue
		}
		if f.UncompressedSize64 > maxExtFile {
			return nil, fmt.Errorf("单文件过大: %s", f.Name)
		}
		total += int64(f.UncompressedSize64)
		if total > maxExtTotal {
			return nil, errors.New("解压后总体积超限")
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(io.LimitReader(rc, maxExtFile+1))
		rc.Close()
		if err != nil {
			return nil, err
		}
		if int64(len(b)) > maxExtFile {
			return nil, fmt.Errorf("单文件过大: %s", f.Name)
		}
		out = append(out, extFileEntry{Name: f.Name, Data: b})
	}
	return out, nil
}

// read7z 依赖系统 7z 二进制;不存在则报错引导改用 zip。
func read7z(data []byte) ([]extFileEntry, error) {
	bin, err := exec.LookPath("7z")
	if err != nil {
		if bin, err = exec.LookPath("7za"); err != nil {
			if bin, err = exec.LookPath("7zr"); err != nil {
				return nil, errors.New("服务器未安装 7z 工具,请改用 .zip 格式上传")
			}
		}
	}
	tmp, err := os.MkdirTemp("", "ext7z-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	src := filepath.Join(tmp, "pkg.7z")
	if err := os.WriteFile(src, data, 0o600); err != nil {
		return nil, err
	}
	dst := filepath.Join(tmp, "x")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "x", "-y", "-o"+dst, src)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("7z 解压失败: %s", strings.TrimSpace(string(out)))
	}
	out := []extFileEntry{}
	var total int64
	err = filepath.Walk(dst, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		// 7z 可以解出符号链接/设备文件:直接拒绝,避免把软链目标(如 /etc/passwd)当普通文件读进扩展目录。
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("7z 包内不允许符号链接")
		}
		if !info.Mode().IsRegular() {
			return errors.New("7z 包内含非普通文件")
		}
		if len(out) >= 200 {
			return errors.New("包内文件过多(>200)")
		}
		rel, _ := filepath.Rel(dst, p)
		if info.Size() > maxExtFile {
			return fmt.Errorf("单文件过大: %s", filepath.ToSlash(rel))
		}
		total += info.Size()
		if total > maxExtTotal {
			return errors.New("解压后总体积超限")
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out = append(out, extFileEntry{Name: filepath.ToSlash(rel), Data: b})
		return nil
	})
	return out, err
}

// safeJoin 防 zip-slip:拒绝 .. 与绝对路径,限制在 root 内。
func safeJoin(root, name string) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	if strings.HasPrefix(name, "/") || strings.Contains(name, "\x00") {
		return "", errors.New("非法路径")
	}
	clean := path.Clean(name)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", errors.New("非法路径")
	}
	full := filepath.Join(root, filepath.FromSlash(clean))
	rootAbs, _ := filepath.Abs(root)
	fullAbs, _ := filepath.Abs(full)
	if fullAbs != rootAbs && !strings.HasPrefix(fullAbs, rootAbs+string(os.PathSeparator)) {
		return "", errors.New("非法路径")
	}
	return full, nil
}

func (a *App) UploadExtension(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(maxExtPackage); err != nil {
		a.fail(w, http.StatusBadRequest, "invalid multipart")
		return
	}
	f, hdr, err := r.FormFile("file")
	if err != nil {
		a.fail(w, http.StatusBadRequest, "file required")
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxExtPackage+1))
	if err != nil || len(data) > maxExtPackage {
		a.fail(w, http.StatusRequestEntityTooLarge, "包体过大(>10MB)")
		return
	}
	ext := strings.ToLower(filepath.Ext(hdr.Filename))
	var files []extFileEntry
	switch ext {
	case ".zip":
		files, err = readZip(data)
	case ".7z":
		files, err = read7z(data)
	default:
		a.fail(w, http.StatusBadRequest, "仅支持 .zip 或 .7z")
		return
	}
	if err != nil {
		a.fail(w, http.StatusBadRequest, "解压失败: "+err.Error())
		return
	}

	// 拍平:兼容"包内多套一层目录"的情况——以包含 info.json 的最浅层为根。
	byName := map[string][]byte{}
	for _, fe := range files {
		byName[strings.ReplaceAll(fe.Name, "\\", "/")] = fe.Data
	}
	// TypeScript 支持:把包内 .ts 编译成同名 .js,并改写 index.html 的引用
	if err := compileExtensionSources(byName); err != nil {
		a.fail(w, http.StatusUnprocessableEntity, "TypeScript 编译失败: "+err.Error())
		return
	}
	root := ""
	for name := range byName {
		if path.Base(name) == "info.json" {
			dir := path.Dir(name)
			if dir == "." {
				dir = ""
			}
			if root == "" || len(dir) < len(root) {
				root = dir
			}
		}
	}
	infoRaw, ok := byName["info.json"]
	if !ok && root != "" {
		infoRaw, ok = byName[root+"/info.json"]
	}
	if !ok {
		a.fail(w, http.StatusBadRequest, "包内缺少 info.json")
		return
	}
	var info extInfo
	infoRaw = []byte(strings.TrimPrefix(string(infoRaw), "\xef\xbb\xbf")) // 容忍 UTF-8 BOM
	if err := json.Unmarshal(infoRaw, &info); err != nil {
		a.fail(w, http.StatusBadRequest, "info.json 不是合法 JSON")
		return
	}
	if strings.TrimSpace(info.Name) == "" || strings.TrimSpace(info.Author) == "" ||
		strings.TrimSpace(info.Version) == "" || strings.TrimSpace(info.Date) == "" {
		a.fail(w, http.StatusBadRequest, "info.json 缺少必填字段(name/author/version/date)")
		return
	}
	if info.Request == nil {
		info.Request = []string{}
	}
	// hook 校验:声明了不存在的 hook 即阻止安装
	if missing := ValidateHooks(info.Request); len(missing) > 0 {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"error":         "插件声明的 hook 在当前后端不可用,已阻止安装",
			"missing_hooks": missing,
		})
		return
	}
	// 包内必须存在 index.html(详情页)与 init.js/init.ts(首次安装脚本,可为空文件)
	prefix := ""
	if root != "" {
		prefix = root + "/"
	}
	if _, ok := byName[prefix+"index.html"]; !ok {
		a.fail(w, http.StatusBadRequest, "包内缺少 index.html")
		return
	}
	hasInit := false
	for _, n := range []string{"init.js", "init.ts"} {
		if _, ok := byName[prefix+n]; ok {
			hasInit = true
			break
		}
	}
	if !hasInit {
		a.fail(w, http.StatusBadRequest, "包内缺少 init.js 或 init.ts")
		return
	}

	slug := extSlug(info.Name)
	if s := strings.TrimSpace(info.Slug); s != "" {
		if !extExplicitSlugRe.MatchString(s) {
			a.fail(w, http.StatusBadRequest, "info.json 的 slug 字段非法(仅允许 a-z0-9-,≤48 字符)")
			return
		}
		slug = s
	}
	if _, err := a.DB.ExtensionBySlug(r.Context(), slug); err == nil {
		a.fail(w, http.StatusConflict, "同名插件已存在,请先卸载旧版本")
		return
	}

	if err := a.ensureExtRoot(); err != nil {
		a.failInternal(w, r, "prepare extensions dir", err)
		return
	}
	dir := a.extDir(slug)
	_ = os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		a.failInternal(w, r, "create extension dir", err)
		return
	}
	for name, b := range byName {
		if prefix != "" {
			if !strings.HasPrefix(name, prefix) {
				continue
			}
			name = strings.TrimPrefix(name, prefix)
		}
		dest, err := safeJoin(dir, name)
		if err != nil {
			continue // 丢弃可疑路径,不中断安装
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			continue
		}
		mode := os.FileMode(0o644)
		if strings.HasPrefix(name, "server/bin/") && (strings.HasSuffix(name, "-server") || strings.HasSuffix(name, "-server.exe")) {
			mode = 0o755
		}
		if err := os.WriteFile(dest, b, mode); err != nil {
			a.failInternal(w, r, "write extension file", err)
			return
		}
	}
	if _, err := pluginrpc.LoadManifest(dir); err == nil {
		if err := pluginrpc.ValidateManifest(dir, slug, false); err != nil {
			_ = os.RemoveAll(dir)
			a.fail(w, http.StatusUnprocessableEntity, "server manifest invalid: "+err.Error())
			return
		}
	}

	id, err := a.DB.CreateExtension(r.Context(), repo.Extension{
		Slug: slug, Name: info.Name, Author: info.Author, Version: info.Version, Date: info.Date,
		Description: info.Description, URL: info.URL, UpdateURL: info.UpdateURL, Favico: info.Favico,
		Hooks: info.Request,
	})
	if err != nil {
		_ = os.RemoveAll(dir)
		a.failInternal(w, r, "create extension", err)
		return
	}
	a.publicCache.clear()
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "slug": slug, "initialized": false})
}

// ---------- 列表/详情 ----------

func (a *App) ListExtensions(w http.ResponseWriter, r *http.Request) {
	items, err := a.DB.ListExtensions(r.Context())
	if err != nil {
		a.failInternal(w, r, "list extensions", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "hooks": HookNames()})
}

func (a *App) GetExtension(w http.ResponseWriter, r *http.Request) {
	e, err := a.DB.ExtensionBySlug(r.Context(), chi.URLParam(r, "slug"))
	if errors.Is(err, repo.ErrNotFound) {
		a.fail(w, http.StatusNotFound, "插件不存在")
		return
	}
	if err != nil {
		a.failInternal(w, r, "get extension", err)
		return
	}
	writeJSON(w, http.StatusOK, e)
}

func (a *App) SetExtensionEnabled(w http.ResponseWriter, r *http.Request) {
	e, err := a.DB.ExtensionBySlug(r.Context(), chi.URLParam(r, "slug"))
	if errors.Is(err, repo.ErrNotFound) {
		a.fail(w, http.StatusNotFound, "插件不存在")
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := decodeJSON(r, &req); err != nil {
		a.fail(w, http.StatusBadRequest, "bad payload")
		return
	}
	if req.Enabled {
		if !e.Initialized {
			a.fail(w, http.StatusConflict, "插件尚未完成初始化,请先在详情页执行 init")
			return
		}
		if err := pluginrpc.ValidateManifest(a.extDir(e.Slug), e.Slug, true); err != nil {
			a.fail(w, http.StatusConflict, "可信服务端插件未通过启用检查: "+err.Error())
			return
		}
	}
	if err := a.DB.SetExtensionEnabled(r.Context(), e.ID, req.Enabled); err != nil {
		a.failInternal(w, r, "set extension enabled", err)
		return
	}
	a.publicCache.clear()
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) MarkExtensionInitialized(w http.ResponseWriter, r *http.Request) {
	e, err := a.DB.ExtensionBySlug(r.Context(), chi.URLParam(r, "slug"))
	if errors.Is(err, repo.ErrNotFound) {
		a.fail(w, http.StatusNotFound, "插件不存在")
		return
	}
	if err := pluginrpc.ValidateManifest(a.extDir(e.Slug), e.Slug, false); err != nil {
		a.fail(w, http.StatusConflict, "插件服务端 manifest 无效: "+err.Error())
		return
	}
	if err := a.DB.SetExtensionInitialized(r.Context(), e.ID); err != nil {
		a.failInternal(w, r, "mark initialized", err)
		return
	}
	a.publicCache.clear()
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) DeleteExtension(w http.ResponseWriter, r *http.Request) {
	e, err := a.DB.ExtensionBySlug(r.Context(), chi.URLParam(r, "slug"))
	if errors.Is(err, repo.ErrNotFound) {
		a.fail(w, http.StatusNotFound, "插件不存在")
		return
	}
	if blocked, err := a.DB.HasS3MediaReferences(r.Context()); err != nil {
		a.failInternal(w, r, "check media references", err)
		return
	} else if blocked && e.Slug == "s3-storage" {
		a.fail(w, http.StatusConflict, "S3 媒体或派生图仍有引用，不能删除")
		return
	}
	if err := a.DB.DeleteExtension(r.Context(), e.ID); err != nil {
		a.failInternal(w, r, "delete extension", err)
		return
	}
	a.publicCache.clear()
	_ = os.RemoveAll(a.extDir(e.Slug))
	w.WriteHeader(http.StatusNoContent)
}

// ---------- 检查更新 ----------

func (a *App) CheckExtensionUpdate(w http.ResponseWriter, r *http.Request) {
	e, err := a.DB.ExtensionBySlug(r.Context(), chi.URLParam(r, "slug"))
	if errors.Is(err, repo.ErrNotFound) {
		a.fail(w, http.StatusNotFound, "插件不存在")
		return
	}
	if e.UpdateURL == "" {
		a.fail(w, http.StatusBadRequest, "该插件未配置 update_url")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	// 配置了自定义更新渠道时,统一走全局清单(plugins.<slug>);否则用插件自己的 update_url。
	var data []byte
	if strings.TrimSpace(a.Cfg.UpdateManifestURL) != "" {
		m, err := a.fetchManifest(ctx)
		if err != nil {
			a.fail(w, http.StatusBadGateway, "检查更新失败: "+err.Error())
			return
		}
		entry, ok := m.Plugins[e.Slug]
		if !ok {
			a.fail(w, http.StatusNotFound, "更新清单中没有该插件")
			return
		}
		remote := repo.ExtUpdate{
			Version: entry.Version, DownloadURL: entry.DownloadURL, PageURL: entry.PageURL,
			Notes: entry.Notes, Date: entry.Date,
		}
		remote.HasUpdate = entry.Version != "" && entry.Version != e.Version
		writeJSON(w, http.StatusOK, remote)
		return
	}
	// 用探测客户端:update_url 来自插件元数据,必须拦内网/回环并校验重定向目标(防 SSRF)。
	data, err = fetchUpdateJSON(ctx, e.UpdateURL)
	if err != nil {
		a.fail(w, http.StatusBadGateway, "检查更新失败: "+err.Error())
		return
	}
	remote, err := decodeExtUpdate(e.Slug, data)
	if err != nil {
		a.fail(w, http.StatusBadGateway, err.Error())
		return
	}
	hasUpdate := remote.Version != "" && remote.Version != e.Version
	remote.HasUpdate = hasUpdate
	writeJSON(w, http.StatusOK, remote)
}

// decodeExtUpdate 兼容两种 update_url 响应格式:
//  1. 仓库共享清单 {"core": {...}, "plugins": {"<slug>": {...}}}(推荐,所有插件指向同一文件)
//  2. 旧扁平格式 {"version","download_url","date"}(单个插件独立托管)
func decodeExtUpdate(slug string, data []byte) (repo.ExtUpdate, error) {
	var m updateManifest
	if err := json.Unmarshal(data, &m); err == nil {
		if entry, ok := m.Plugins[slug]; ok {
			return repo.ExtUpdate{
				Version: entry.Version, DownloadURL: entry.DownloadURL,
				PageURL: entry.PageURL, Notes: entry.Notes, Date: entry.Date,
			}, nil
		}
		if m.Core.Version != "" || len(m.Plugins) > 0 {
			return repo.ExtUpdate{}, errors.New("更新清单中没有该插件")
		}
	}
	var flat struct {
		Version     string `json:"version"`
		DownloadURL string `json:"download_url"`
		PageURL     string `json:"page_url"`
		Notes       string `json:"notes"`
		Date        string `json:"date"`
	}
	if err := json.Unmarshal(data, &flat); err != nil || flat.Version == "" {
		return repo.ExtUpdate{}, errors.New("更新地址返回格式错误(期望 version 或 plugins 清单)")
	}
	return repo.ExtUpdate{
		Version: flat.Version, DownloadURL: flat.DownloadURL,
		PageURL: flat.PageURL, Notes: flat.Notes, Date: flat.Date,
	}, nil
}

// ---------- 插件静态资源(page/assets) ----------

// extBridgeScript 注入到插件 index.html 的桥接运行时:
//
//	Fanke.callHook(name, payload) → 由父页面代发鉴权请求并把结果回传;
//	Fanke.initDone() → 通知父页面 init.js 已执行完成;
//	Fanke.isInit → 当前是否处于首次安装初始化流程;
//	Fanke.slug → 插件 slug。
const extBridgeScript = `<script>(function(){
let seq=0;const pending=new Map();
window.addEventListener('message',function(ev){
  var d=ev.data;if(!d||d.type!=='fanke:hook:result')return;
  var p=pending.get(d.id);if(!p)return;pending.delete(d.id);
  if(d.ok)p.resolve(d.data);else p.reject(new Error(d.error||'hook 调用失败'));
});
var q=new URLSearchParams(location.search);
var parts=location.pathname.split('/');
window.Fanke={
  slug:parts[parts.indexOf('ext')+1]||'',
  isInit:q.get('init')==='1',
  callHook:function(name,payload){return new Promise(function(resolve,reject){
    var id=++seq;pending.set(id,{resolve:resolve,reject:reject});
    parent.postMessage({type:'fanke:hook',id:id,hook:name,payload:payload},location.origin);
  });},
  initDone:function(){parent.postMessage({type:'fanke:init-done'},location.origin);},
  emit:function(type,detail){parent.postMessage({type:'fanke:emit',ev:type,detail:detail},location.origin);}
};
})();</script>`

// injectBridge 把桥接脚本插到 <head> 之后;无 <head> 则前置。init=1 时追加 init.js。
func injectBridge(html []byte, isInit bool) []byte {
	s := string(html)
	// 幂等:以桥接脚本独有标记判断;不能查 "Fanke"(插件业务代码必然引用 Fanke.callHook)。
	if !strings.Contains(s, "fanke:hook:result") {
		if i := strings.Index(strings.ToLower(s), "<head>"); i >= 0 {
			s = s[:i+6] + extBridgeScript + s[i+6:]
		} else {
			s = extBridgeScript + s
		}
	}
	if isInit {
		lower := strings.ToLower(s)
		if i := strings.LastIndex(lower, "</body>"); i >= 0 {
			s = s[:i] + `<script src="assets/init.js"></script>` + s[i:]
		} else {
			s += `<script src="assets/init.js"></script>`
		}
	}
	return []byte(s)
}

// ServeExtensionPage 返回插件 index.html(公开访问;页面自身不敏感,敏感操作走 hook 桥)。
func (a *App) ServeExtensionPage(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	if !extExplicitSlugRe.MatchString(slug) {
		a.fail(w, http.StatusBadRequest, "非法插件标识")
		return
	}
	p := filepath.Join(a.extDir(slug), "index.html")
	data, err := os.ReadFile(p)
	if err != nil {
		a.fail(w, http.StatusNotFound, "插件页面不存在")
		return
	}
	data = injectBridge(data, r.URL.Query().Get("init") == "1")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	// 允许同源 iframe 嵌入(覆盖全局 X-Frame-Options: DENY);CSP frame-ancestors 对现代浏览器生效。
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data: https: http:; connect-src 'self'; frame-ancestors 'self'")
	_, _ = w.Write(data)
}

// ServeExtensionAsset 提供包内其他静态文件(favico/css/js/图片等)。
func (a *App) ServeExtensionAsset(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	if !extExplicitSlugRe.MatchString(slug) {
		a.fail(w, http.StatusBadRequest, "非法插件标识")
		return
	}
	rest := chi.URLParam(r, "*")
	p, err := safeJoin(a.extDir(slug), rest)
	if err != nil {
		a.fail(w, http.StatusBadRequest, "非法路径")
		return
	}
	// 运行时兜底:只有 .ts 没有 .js 时现场编译(老插件升级后无需重装)
	if js, compiled := ensureExtensionJS(a.extDir(slug), rest); compiled {
		p = js
	}
	fi, err := os.Stat(p)
	if err != nil || fi.IsDir() {
		a.fail(w, http.StatusNotFound, "资源不存在")
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=3600")
	http.ServeFile(w, r, p)
}

// ServeExtensionPageFile 让插件 index.html 里的相对 URL(init.js、style.css 等)可解析。
func (a *App) ServeExtensionPageFile(w http.ResponseWriter, r *http.Request) {
	a.ServeExtensionAsset(w, r)
}

// ---------- hook 调用 ----------

// InvokeExtensionHookAdmin 管理端调用(父页面 postMessage 桥转发到这里,带 Bearer)。
func (a *App) InvokeExtensionHookAdmin(w http.ResponseWriter, r *http.Request) {
	a.invokeExtensionHook(w, r, false)
}

// InvokeExtensionHookPublic 公开调用(仅 Public=true 的 hook,如 get_batch)。
func (a *App) InvokeExtensionHookPublic(w http.ResponseWriter, r *http.Request) {
	a.invokeExtensionHook(w, r, true)
}

func (a *App) invokeExtensionHook(w http.ResponseWriter, r *http.Request, isPublic bool) {
	slug := chi.URLParam(r, "slug")
	hookName := chi.URLParam(r, "hook")
	e, err := a.DB.ExtensionBySlug(r.Context(), slug)
	if errors.Is(err, repo.ErrNotFound) {
		a.fail(w, http.StatusNotFound, "插件不存在")
		return
	}
	// 启用状态只约束公开调用;管理端调用始终放行(详情页/初始化都依赖它)。
	if isPublic && !e.Enabled {
		a.fail(w, http.StatusConflict, "插件未启用")
		return
	}
	// 每个插件对每个 hook 的调用频率上限(按分钟计,管理端与公开通道共用同一个桶)
	if a.Limits != nil && a.Limits.Hook != nil && !a.Limits.Hook.allow(slug, hookName) {
		w.Header().Set("Retry-After", "60")
		a.fail(w, http.StatusTooManyRequests,
			fmt.Sprintf("hook 调用过于频繁: %s(每个插件上限 %d 次/分钟)", hookName, hookPerMinute(hookName)))
		return
	}
	var payload json.RawMessage
	if r.Body != nil {
		b, _ := io.ReadAll(io.LimitReader(r.Body, 4<<20))
		if len(b) > 0 {
			payload = b
		}
	}
	out, err := a.InvokeHook(r.Context(), e, hookName, payload, isPublic)
	if err != nil {
		a.fail(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": out})
}

// ---------- 启用状态查询(前台用) ----------

// ListEnabledExtensions 公开:返回启用中的插件 slug 列表,以及声明了编辑器能力的插件。
// 编辑器能力必须在 info.json 的 request 中声明 editor.register,未声明的插件不会被后台编辑器加载。
func (a *App) ListEnabledExtensions(w http.ResponseWriter, r *http.Request) {
	type publicExt struct {
		Enabled []string `json:"enabled"`
		Editors []string `json:"editors"`
	}
	out, err := cachedPublicJSON(r.Context(), &a.publicCache, cacheEnabledExtensions, func() (publicExt, error) {
		res := publicExt{Enabled: []string{}, Editors: []string{}}
		items, err := a.DB.ListExtensions(r.Context())
		if err != nil {
			return res, err
		}
		for _, e := range items {
			if !e.Enabled || !e.Initialized {
				continue
			}
			res.Enabled = append(res.Enabled, e.Slug)
			for _, h := range e.Hooks {
				if h == EditorRegister {
					res.Editors = append(res.Editors, e.Slug)
					break
				}
			}
		}
		sort.Strings(res.Enabled)
		sort.Strings(res.Editors)
		return res, nil
	})
	if err != nil {
		a.failInternal(w, r, "list extensions", err)
		return
	}
	writeJSONCached(w, r, http.StatusOK, out)
}
