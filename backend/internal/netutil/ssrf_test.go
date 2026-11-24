package netutil

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestIsForbiddenIP(t *testing.T) {
	forbidden := []string{
		"0.0.0.0",
		"10.0.0.1",
		"100.64.0.1",
		"127.0.0.1",
		"127.255.255.254",
		"169.254.169.254",
		"172.16.0.1",
		"172.31.255.254",
		"192.168.1.1",
		"192.0.2.1",
		"198.18.0.1",
		"198.51.100.1",
		"203.0.113.1",
		"224.0.0.1",
		"255.255.255.255",
		"::",
		"::1",
		"::ffff:127.0.0.1",
		"::ffff:169.254.169.254",
		"fc00::1",
		"fd12:3456::1",
		"fe80::1",
		"ff02::1",
		"64:ff9b::1",
		"2001:db8::1",
		"2002::1",
	}
	for _, s := range forbidden {
		ip := net.ParseIP(s)
		if !IsForbiddenIP(ip) {
			t.Errorf("IsForbiddenIP(%s) = false, want true", s)
		}
	}
	if !IsForbiddenIP(nil) {
		t.Error("IsForbiddenIP(nil) = false, want true")
	}

	allowed := []string{
		"1.1.1.1",
		"8.8.8.8",
		"93.184.216.34",
		"172.15.255.254",
		"172.32.0.1",
		"2606:4700:4700::1111",
	}
	for _, s := range allowed {
		ip := net.ParseIP(s)
		if IsForbiddenIP(ip) {
			t.Errorf("IsForbiddenIP(%s) = true, want false", s)
		}
	}
}

func TestAllowlistMatching(t *testing.T) {
	al := ParseAllowlist("127.0.0.1, localhost, 192.168.0.0/16, *.internal, ollama.local:11434")
	if al.AllowsAll() {
		t.Fatal("should not allow all")
	}
	if !al.Allowed("127.0.0.1", net.ParseIP("127.0.0.1")) {
		t.Error("127.0.0.1 should be allowed")
	}
	if !al.Allowed("localhost", net.ParseIP("127.0.0.1")) {
		t.Error("localhost should be allowed")
	}
	if !al.Allowed("minio.lan", net.ParseIP("192.168.10.20")) {
		t.Error("192.168.10.20 CIDR should be allowed")
	}
	if !al.Allowed("api.internal", net.ParseIP("10.0.0.2")) {
		t.Error("*.internal wildcard should allow api.internal")
	}
	if !al.Allowed("ollama.local", net.ParseIP("10.0.0.3")) {
		t.Error("ollama.local with stripped port should be allowed")
	}
	if al.Allowed("evil.com", net.ParseIP("169.254.169.254")) {
		t.Error("metadata IP should NOT be allowed")
	}
	if al.Allowed("notinternal", net.ParseIP("10.0.0.2")) {
		t.Error("notinternal should NOT match *.internal")
	}

	star := ParseAllowlist(" , * , ")
	if !star.AllowsAll() || !star.Allowed("anything", net.ParseIP("169.254.169.254")) {
		t.Error("wildcard * should allow all")
	}
}

func TestSafeDialContextAllowlistAndBlock(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	doReq := func(allowlist string) error {
		cli := &http.Client{
			Timeout: 3 * time.Second,
			Transport: &http.Transport{
				Proxy:       nil,
				DialContext: SafeDialContext(allowlist),
			},
		}
		resp, err := cli.Get(srv.URL)
		if err != nil {
			return err
		}
		resp.Body.Close()
		return nil
	}

	if err := doReq(""); err == nil || !errors.Is(err, ErrForbiddenAddress) {
		t.Fatalf("default SafeDialContext should block loopback, got err=%v", err)
	}
	if err := doReq("192.168.0.0/16"); err == nil || !errors.Is(err, ErrForbiddenAddress) {
		t.Fatalf("non-matching allowlist should block loopback, got err=%v", err)
	}
	for _, allow := range []string{"127.0.0.1", "127.0.0.0/8", "*"} {
		if err := doReq(allow); err != nil {
			t.Fatalf("allowlist=%q should permit loopback, got err=%v", allow, err)
		}
	}
}

func TestValidateURLAndResolveSafeIP(t *testing.T) {
	if _, err := ValidateURL("http://169.254.169.254/latest/meta-data", "", false); !errors.Is(err, ErrForbiddenAddress) {
		t.Fatalf("metadata URL should be blocked: %v", err)
	}
	if _, err := ValidateURL("http://localhost:11434", "", false); !errors.Is(err, ErrForbiddenAddress) {
		t.Fatalf("localhost URL should be blocked by default: %v", err)
	}
	if _, err := ValidateURL("http://localhost:11434", "localhost", false); err != nil {
		t.Fatalf("localhost URL should be allowed when whitelisted: %v", err)
	}
	if _, err := ValidateURL("http://8.8.8.8/updates.json", "", true); err == nil {
		t.Fatal("requireHTTPS=true should reject http://")
	}
	if _, err := ValidateURL("https://8.8.8.8/updates.json", "", true); err != nil {
		t.Fatalf("https public IP should pass: %v", err)
	}

	ctx := context.Background()
	if _, _, _, err := ResolveSafeIP(ctx, "http://8.8.8.8/a.tar.gz", false); err == nil {
		t.Fatal("ResolveSafeIP with allowHTTP=false must reject http://")
	}
	host, port, ip, err := ResolveSafeIP(ctx, "https://8.8.8.8/a.tar.gz", false)
	if err != nil || host != "8.8.8.8" || port != "443" || ip != "8.8.8.8" {
		t.Fatalf("ResolveSafeIP https public = (%q,%q,%q,%v)", host, port, ip, err)
	}
	if _, _, _, err := ResolveSafeIP(ctx, "https://127.0.0.1/a.tar.gz", false); !errors.Is(err, ErrForbiddenAddress) {
		t.Fatalf("ResolveSafeIP loopback must fail: %v", err)
	}
	if _, _, _, err := ResolveSafeIP(ctx, "https://localhost/a.tar.gz", false); !errors.Is(err, ErrForbiddenAddress) {
		t.Fatalf("ResolveSafeIP localhost must fail: %v", err)
	}
	if got := FormatCurlResolve("example.com", "443", "93.184.216.34"); got != "example.com:443:93.184.216.34" {
		t.Fatalf("FormatCurlResolve ipv4 = %q", got)
	}
	if got := FormatCurlResolve("example.com", "443", "2606:4700::1"); got != "example.com:443:[2606:4700::1]" {
		t.Fatalf("FormatCurlResolve ipv6 = %q", got)
	}
}
