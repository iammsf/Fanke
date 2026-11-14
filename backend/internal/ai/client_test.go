package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
