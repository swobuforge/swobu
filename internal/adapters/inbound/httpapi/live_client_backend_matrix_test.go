//go:build integration_live
// +build integration_live

package httpapi_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

type liveMatrixResponse struct {
	Error map[string]any `json:"error"`
}

func TestLiveClientBackendHelloMatrix(t *testing.T) {
	if strings.TrimSpace(os.Getenv("SWOBU_LIVE_SMOKE")) != "1" { // swobu:io-string source=domain
		t.Skip("set SWOBU_LIVE_SMOKE=1 to run live client/backend smoke matrix") // swobu:lint ignore no-test-skip because=live smoke requires opt-in environment
	}

	daemonURL := strings.TrimSpace(os.Getenv("SWOBU_LIVE_DAEMON_URL")) // swobu:io-string source=domain
	if daemonURL == "" {
		daemonURL = "http://127.0.0.1:7926"
	}
	daemonURL = strings.TrimRight(daemonURL, "/")

	backends := []string{
		envOrDefault(t, "SWOBU_LIVE_ENDPOINT_OPENROUTER", "openrouter"),
		envOrDefault(t, "SWOBU_LIVE_ENDPOINT_OPENAI", "openai"),
		envOrDefault(t, "SWOBU_LIVE_ENDPOINT_ANTHROPIC", "anthropic"),
		envOrDefault(t, "SWOBU_LIVE_ENDPOINT_CHATGPT", "chatgpt"),
	}
	backendLabel := []string{"openrouter", "openai", "anthropic", "chatgpt"}

	clients := []struct {
		id      string
		ua      string
		path    string
		payload string
	}{
		{
			id:      "codex",
			ua:      "Codex/1.0",
			path:    "/chat/completions",
			payload: `{"model":"swobu","messages":[{"role":"user","content":"hello world"}],"stream":true}`,
		},
		{
			id:      "aider",
			ua:      "Aider/0.82",
			path:    "/chat/completions",
			payload: `{"model":"swobu","messages":[{"role":"user","content":"hello world"}],"stream":true}`,
		},
		{
			id:      "opencode",
			ua:      "OpenCode/0.1",
			path:    "/chat/completions",
			payload: `{"model":"swobu","messages":[{"role":"user","content":"hello world"}],"stream":true}`,
		},
		{
			id:      "claude",
			ua:      "Claude-Code/2.0",
			path:    "/messages",
			payload: `{"model":"swobu","messages":[{"role":"user","content":"hello world"}],"stream":true}`,
		},
	}

	httpClient := &http.Client{Timeout: 20 * time.Second}
	for bi, endpoint := range backends {
		endpoint = strings.TrimSpace(endpoint) // swobu:io-string source=domain
		for _, client := range clients {
			testName := fmt.Sprintf("%s_%s", client.id, backendLabel[bi])
			t.Run(testName, func(t *testing.T) {
				t.Parallel()
				reqID := fmt.Sprintf("live_%s_%s_%d", client.id, backendLabel[bi], time.Now().UnixNano())
				statusCode, body := postClientHello(t, httpClient, daemonURL, endpoint, client.path, client.payload, client.ua, reqID)
				if statusCode != http.StatusOK {
					var errBody liveMatrixResponse
					_ = json.Unmarshal(body, &errBody)
					t.Fatalf("status=%d body=%s parsed_error=%v", statusCode, string(body), errBody.Error)
				}
				if len(bytes.TrimSpace(body)) == 0 {
					t.Fatalf("empty response body for %s/%s", client.id, backendLabel[bi])
				}
			})
		}
	}
}

func postClientHello(
	t *testing.T,
	client *http.Client,
	daemonURL, endpoint, path, payload, userAgent, requestID string,
) (int, []byte) {
	t.Helper()
	url := daemonURL + "/c/" + endpoint + path
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(payload))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("X-Request-Id", requestID)
	if strings.HasSuffix(path, "/messages") {
		req.Header.Set("anthropic-version", "2023-06-01")
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("client.Do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll response body: %v", err)
	}
	return resp.StatusCode, body
}

func requireEnv(t *testing.T, key string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(key)) // swobu:io-string source=domain
	if value == "" {
		t.Fatalf("required env %s is empty", key)
	}
	return value
}

func envOrDefault(t *testing.T, key string, fallback string) string {
	t.Helper()
	if value := strings.TrimSpace(os.Getenv(key)); value != "" { // swobu:io-string source=domain
		return value
	}
	return fallback
}
