package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"fanke/backend/internal/repo"
)

func testClient() *Client { return &Client{hc: http.DefaultClient} }

func TestListModelsClaudeUsesV1AndPagination(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.String())
		if r.Header.Get("x-api-key") != "secret" || r.Header.Get("anthropic-version") != "2023-06-01" {
			t.Errorf("missing Claude headers")
		}
		if r.URL.Query().Get("after_id") == "" {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"id": "a"}}, "has_more": true, "last_id": "cursor"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"id": "b"}}, "has_more": false})
	}))
	defer srv.Close()
	models, err := testClient().ListModels(context.Background(), "claude", srv.URL+"/v1/", "secret")
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || paths[0] != "/v1/models" || paths[1] != "/v1/models?after_id=cursor" {
		t.Fatalf("models=%v paths=%v", models, paths)
	}
}

func TestChatClaudeNormalizesURLAndPayload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		var body struct {
			System    string    `json:"system"`
			MaxTokens int       `json:"max_tokens"`
			Messages  []Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.System != "be concise" || body.MaxTokens != 1024 || len(body.Messages) != 1 || body.Messages[0].Role == "system" {
			t.Fatalf("payload=%+v", body)
		}
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"one"},{"type":"tool_use","text":"ignored"},{"type":"text","text":"two"}],"usage":{"input_tokens":2,"output_tokens":3}}`))
	}))
	defer srv.Close()
	resp, err := testClient().chatClaude(context.Background(), repoConn(srv.URL+"/v1/"), ChatRequest{Messages: []Message{{Role: "system", Content: "be concise"}, {Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "onetwo" || resp.Usage.TotalTokens != 5 {
		t.Fatalf("response=%+v", resp)
	}
}

func repoConn(apiURL string) repo.AIConnection {
	return repo.AIConnection{APIURL: apiURL, APIKey: "secret", Model: "claude-test"}
}

func TestNewClientBlocksLoopbackByDefault(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"id": "local-model"}}})
	}))
	defer srv.Close()

	if _, err := NewClient(nil).ListModels(context.Background(), "openai", srv.URL, "secret"); err == nil {
		t.Fatal("default AI client must block loopback SSRF")
	}
	models, err := NewClientWithAllowlist(nil, "127.0.0.1").ListModels(context.Background(), "openai", srv.URL, "secret")
	if err != nil {
		t.Fatalf("whitelisted loopback AI client failed: %v", err)
	}
	if len(models) != 1 || models[0] != "local-model" {
		t.Fatalf("models = %v", models)
	}
}

func TestNewClientBlocksRedirectToMetadata(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	}))
	defer srv.Close()

	// 即使放行了 127.0.0.1,重定向到未放行的云元数据地址也必须被拦截。
	if _, err := NewClientWithAllowlist(nil, "127.0.0.1").ListModels(context.Background(), "openai", srv.URL, "secret"); err == nil {
		t.Fatal("redirect to cloud metadata should be blocked")
	}
}

func TestResponseErrorDoesNotLeakUpstreamBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("INTERNAL_SECRET_DIAGNOSTIC_PAYLOAD"))
	}))
	defer srv.Close()

	_, err := NewClientWithAllowlist(nil, "127.0.0.1").chatOpenAI(context.Background(), repoConn(srv.URL), ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error on HTTP 500")
	}
	if strings.Contains(err.Error(), "INTERNAL_SECRET_DIAGNOSTIC_PAYLOAD") {
		t.Fatalf("upstream response body leaked in error: %v", err)
	}
}
