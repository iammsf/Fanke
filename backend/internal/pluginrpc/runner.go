package pluginrpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const ProtocolVersion = 1
const MaxResponseBytes = 4 << 20

type Request struct {
	Version int    `json:"version"`
	ID      string `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}
type Response struct {
	Version int             `json:"version"`
	ID      string          `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   string          `json:"error,omitempty"`
}
type Runner struct {
	Entrypoint string
	Root       string
	Timeout    time.Duration
}

func ValidateEntrypoint(root, entry string) error {
	if root == "" || entry == "" || filepath.IsAbs(entry) || filepath.VolumeName(entry) != "" {
		return errors.New("invalid plugin entrypoint")
	}
	r, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	rootInfo, err := os.Lstat(r)
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return errors.New("plugin root must be a real directory")
	}
	p := filepath.Join(r, filepath.FromSlash(entry))
	rel, err := filepath.Rel(r, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return errors.New("entrypoint escapes plugin root")
	}
	current := r
	for _, part := range strings.Split(rel, string(os.PathSeparator)) {
		current = filepath.Join(current, part)
		fi, err := os.Lstat(current)
		if err != nil {
			return errors.New("plugin entrypoint unavailable")
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return errors.New("plugin entrypoint contains symlink")
		}
	}
	fi, err := os.Stat(p)
	if err != nil || !fi.Mode().IsRegular() {
		return errors.New("entrypoint must be a regular file")
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm()&0111 == 0 {
		return errors.New("plugin entrypoint is not executable")
	}
	return nil
}
func (r Runner) Call(ctx context.Context, method string, params any, result any) error {
	if err := ValidateEntrypoint(r.Root, r.Entrypoint); err != nil {
		return err
	}
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req := Request{Version: ProtocolVersion, ID: fmt.Sprintf("%d", time.Now().UnixNano()), Method: method, Params: params}
	body, err := json.Marshal(req)
	if err != nil {
		return errors.New("invalid plugin request")
	}
	exe, err := filepath.Abs(filepath.Join(r.Root, filepath.FromSlash(r.Entrypoint)))
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, exe)
	cmd.Env = minimalPluginEnv()
	cmd.Stdin = bytes.NewReader(append(body, '\n'))
	stdout := &limitedBuffer{n: MaxResponseBytes}
	cmd.Stdout = stdout
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	err = cmd.Run()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if stdout.exceeded {
		return errors.New("plugin response too large")
	}
	if err != nil {
		return errors.New("plugin process failed")
	}
	var resp Response
	if json.Unmarshal(bytes.TrimSpace(stdout.buf.Bytes()), &resp) != nil {
		return errors.New("invalid plugin response")
	}
	if resp.Version != ProtocolVersion || resp.ID != req.ID {
		return errors.New("plugin response protocol mismatch")
	}
	if resp.Error != "" {
		return fmt.Errorf("plugin %s: %s", method, resp.Error)
	}
	if result != nil && json.Unmarshal(resp.Result, result) != nil {
		return errors.New("invalid plugin result")
	}
	return nil
}

func minimalPluginEnv() []string {
	keys := []string{"PATH", "SystemRoot", "WINDIR", "TEMP", "TMP", "HOME", "LANG", "LC_ALL"}
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		if value, ok := os.LookupEnv(key); ok {
			out = append(out, key+"="+value)
		}
	}
	return out
}

type limitedBuffer struct {
	buf      bytes.Buffer
	n        int
	exceeded bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	size := len(p)
	if size > b.n {
		b.exceeded = true
		return 0, errors.New("plugin response too large")
	}
	b.buf.Write(p)
	b.n -= size
	return size, nil
}
