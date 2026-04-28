package e2e_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newChatCompletionsUpstream(t *testing.T, chatStatus int, chatBody string) *httptest.Server {
	t.Helper()

	if chatStatus <= 0 {
		chatStatus = http.StatusOK
	}
	if chatBody == "" {
		chatBody = `{"id":"chatcmpl_1","object":"chat.completion","created":1,"model":"gpt-4.1-mini","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"gpt-4.1-mini","object":"model"}]}`))
		case "/v1/chat/completions":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(chatStatus)
			_, _ = w.Write([]byte(chatBody))
		default:
			t.Fatalf("unexpected upstream path %q", r.URL.Path)
		}
	}))
}

func postChatCompletion(t *testing.T, url string, body string) (int, string) {
	t.Helper()

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do returned error: %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll returned error: %v", err)
	}
	return resp.StatusCode, string(raw)
}
