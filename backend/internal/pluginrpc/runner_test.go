package pluginrpc

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func buildRunnerFixture(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	code := `package main
import("bufio";"encoding/json";"fmt";"os";"time")
var _ = fmt.Print
var _ = time.Sleep
func main(){s:=bufio.NewScanner(os.Stdin); if !s.Scan(){return}; var q map[string]any; _=json.Unmarshal(s.Bytes(),&q); ` + body + `}
`
	if err := os.WriteFile(src, []byte(code), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "fixture")
	if runtime.GOOS == "windows" {
		out += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", out, src)
	cmd.Env = append(os.Environ(), "GO111MODULE=off")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fixture: %v: %s", err, b)
	}
	return out
}

func callFixture(t *testing.T, body string, timeout time.Duration) error {
	exe := buildRunnerFixture(t, body)
	r := Runner{Root: filepath.Dir(exe), Entrypoint: filepath.Base(exe), Timeout: timeout}
	return r.Call(context.Background(), "test", nil, nil)
}

func TestRunnerTimeoutCrashProtocolAndLimit(t *testing.T) {
	if err := callFixture(t, `time.Sleep(time.Second)`, 20*time.Millisecond); err == nil || !strings.Contains(err.Error(), "deadline") {
		t.Fatalf("timeout: %v", err)
	}
	if err := callFixture(t, `os.Exit(3)`, time.Second); err == nil {
		t.Fatal("crash accepted")
	}
	if err := callFixture(t, `fmt.Print("{\"version\":99}")`, time.Second); err == nil {
		t.Fatal("protocol mismatch accepted")
	}
	if err := callFixture(t, `fmt.Print("`+strings.Repeat("x", MaxResponseBytes+1)+`")`, time.Second); err == nil {
		t.Fatal("oversize accepted")
	}
}

func TestRunnerPathAndSymlinkRejection(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "not-exec"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := (Runner{Root: dir, Entrypoint: "not-exec"}).Call(context.Background(), "test", nil, nil); err == nil {
		t.Fatal("non-executable accepted")
	}
	if err := ValidateEntrypoint(filepath.Join(dir, "missing"), "x"); err == nil {
		t.Fatal("missing root accepted")
	}
	if err := os.Symlink(filepath.Join(dir, "not-exec"), filepath.Join(dir, "link")); err == nil {
		if err := ValidateEntrypoint(dir, "link"); err == nil {
			t.Fatal("symlink accepted")
		}
	}
}
