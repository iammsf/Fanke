package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDefaultsAndAllowlists(t *testing.T) {
	t.Setenv("FANKE_CONFIG", filepath.Join(t.TempDir(), "nonexistent.env"))
	t.Setenv("ADDR", "")
	t.Setenv("ALLOW_PRIVATE_AI_HOSTS", "127.0.0.1,localhost")
	t.Setenv("ALLOW_PRIVATE_S3_HOSTS", "192.168.0.0/16")
	t.Setenv("ALLOW_INSECURE_UPDATE_HTTP", "true")

	cfg := Load()
	if cfg.Addr != "127.0.0.1:8081" {
		t.Fatalf("default ADDR = %q, want 127.0.0.1:8081", cfg.Addr)
	}
	if cfg.AllowPrivateAIHosts != "127.0.0.1,localhost" {
		t.Fatalf("AllowPrivateAIHosts = %q", cfg.AllowPrivateAIHosts)
	}
	if cfg.AllowPrivateS3Hosts != "192.168.0.0/16" {
		t.Fatalf("AllowPrivateS3Hosts = %q", cfg.AllowPrivateS3Hosts)
	}
	if !cfg.AllowInsecureUpdateHTTP {
		t.Fatal("AllowInsecureUpdateHTTP should be true")
	}
}

func TestBuildDBURLValidationAndEncoding(t *testing.T) {
	got, err := BuildDBURL("127.0.0.1", "5432", "fanke", "u:ser", "p@ss#/?%", "require")
	if err != nil {
		t.Fatalf("valid BuildDBURL failed: %v", err)
	}
	if !strings.HasPrefix(got, "postgres://u%3Aser:p%40ss%23%2F%3F%25@127.0.0.1:5432/fanke?sslmode=require") {
		t.Fatalf("unexpected encoded DSN: %s", got)
	}

	for _, mode := range []string{"disable", "prefer", "require", "verify-ca", "verify-full"} {
		if _, err := BuildDBURL("localhost", "5432", "db", "user", "pass", mode); err != nil {
			t.Errorf("valid sslmode %q failed: %v", mode, err)
		}
	}

	badCases := []struct {
		name                                  string
		host, port, dbname, user, pw, sslmode string
	}{
		{"newline in sslmode", "127.0.0.1", "5432", "db", "u", "p", "disable\nJWT_SECRET=evil"},
		{"extra query in sslmode", "127.0.0.1", "5432", "db", "u", "p", "disable&sslrootcert=/tmp/x"},
		{"unknown sslmode", "127.0.0.1", "5432", "db", "u", "p", "allow"},
		{"crlf in dbname", "127.0.0.1", "5432", "db\r\nFOO=1", "u", "p", "disable"},
		{"null in password", "127.0.0.1", "5432", "db", "u", "p\x00x", "disable"},
		{"host injection", "127.0.0.1/evil", "5432", "db", "u", "p", "disable"},
		{"bad port", "127.0.0.1", "99999", "db", "u", "p", "disable"},
	}
	for _, tc := range badCases {
		if _, err := BuildDBURL(tc.host, tc.port, tc.dbname, tc.user, tc.pw, tc.sslmode); err == nil {
			t.Errorf("%s: expected error, got nil", tc.name)
		}
	}
}

func TestWriteFileRejectsInjectionAndEnforces0600(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "fanke.env")

	// 预先创建权限过宽的文件,WriteFile 仍应将其强制收紧至 0600。
	if err := os.WriteFile(p, []byte("OLD=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(p, map[string]string{"DATABASE_URL": "postgres://u:p@127.0.0.1:5432/db?sslmode=disable"}); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Fatalf("perm = %o, want 0600", perm)
	}

	for _, kv := range []map[string]string{
		{"A": "line1\nJWT_SECRET=hacked"},
		{"A": "line1\rline2"},
		{"A": "null\x00byte"},
		{"BAD\nKEY": "val"},
		{"BAD=KEY": "val"},
		{"": "val"},
	} {
		if err := WriteFile(p, kv); err == nil {
			t.Errorf("WriteFile(%v) should fail", kv)
		}
	}
}

func TestIsWeakSecret(t *testing.T) {
	weakJWT := []string{"", "change-me", "CHANGE-ME", "short", "my-change-me-secret-1234567890123456"}
	for _, s := range weakJWT {
		if !IsWeakSecret(s, 32) {
			t.Errorf("IsWeakSecret(%q, 32) = false, want true", s)
		}
	}
	if IsWeakSecret(RandomHex(32), 32) {
		t.Error("RandomHex(32) should not be weak")
	}
	if !IsWeakSecret("change-me", 16) {
		t.Error("change-me should be weak for recovery key")
	}
	if IsWeakSecret(RandomHex(16), 16) {
		t.Error("RandomHex(16) should not be weak")
	}
}
