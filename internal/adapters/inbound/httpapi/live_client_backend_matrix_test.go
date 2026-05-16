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

type liveStatusProjection struct {
	RecentTraffic []liveTrafficRow `json:"recent_traffic"`
}

type liveTrafficRow struct {
	RequestID      string `json:"request_id"`
	Endpoint       string `json:"endpoint"`
	ClientHandler  string `json:"client_handler"`
	ClientProtocol string `json:"client_protocol"`
	IngressFamily  string `json:"ingress_family"`
	Result         string `json:"result"`
	Route          string `json:"route"`
}

func TestLiveClientBackendHelloMatrix(t *testing.T) {
	if strings.TrimSpace(os.Getenv("SWOBU_LIVE_SMOKE")) != "1" {
		t.Skip("set SWOBU_LIVE_SMOKE=1 to run live client/backend smoke matrix")
	}

	daemonURL := strings.TrimSpace(os.Getenv("SWOBU_LIVE_DAEMON_URL"))
	if daemonURL == "" {
		daemonURL = "http://127.0.0.1:7926"
	}
	daemonURL = strings.TrimRight(daemonURL, "/")

	backends := []string{
		requireEnv(t, "SWOBU_LIVE_ENDPOINT_OPENROUTER"),
		requireEnv(t, "SWOBU_LIVE_ENDPOINT_OPENAI"),
		requireEnv(t, "SWOBU_LIVE_ENDPOINT_ANTHROPIC"),
		requireEnv(t, "SWOBU_LIVE_ENDPOINT_CHATGPT"),
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
		endpoint = strings.TrimSpace(endpoint)
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

				row := findTrafficRowByRequestID(t, httpClient, daemonURL, endpoint, reqID)
				if got := strings.TrimSpace(row.ClientHandler); got == "" {
					t.Fatalf("missing client_handler in status projection row for request_id=%s", reqID)
				}
				if got := strings.TrimSpace(row.ClientProtocol); got == "" {
					t.Fatalf("missing client_protocol in status projection row for request_id=%s", reqID)
				}
				if got := strings.TrimSpace(row.Route); got == "" {
					t.Fatalf("missing route in status projection row for request_id=%s", reqID)
				}
				if got := strings.TrimSpace(row.Result); got == "" {
					t.Fatalf("missing result in status projection row for request_id=%s", reqID)
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

func findTrafficRowByRequestID(t *testing.T, client *http.Client, daemonURL, endpoint, requestID string) liveTrafficRow {
	t.Helper()
	url := daemonURL + "/_swobu/status-projection?scope=endpoint:" + endpoint
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("NewRequest projection: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("projection client.Do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("projection status=%d body=%s", resp.StatusCode, string(body))
	}
	var projection liveStatusProjection
	if err := json.NewDecoder(resp.Body).Decode(&projection); err != nil {
		t.Fatalf("projection decode: %v", err)
	}
	for _, row := range projection.RecentTraffic {
		if row.RequestID == requestID {
			return row
		}
	}
	t.Fatalf("request_id=%s not found in recent traffic rows=%v", requestID, projection.RecentTraffic)
	return liveTrafficRow{}
}

func requireEnv(t *testing.T, key string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		t.Fatalf("required env %s is empty", key)
	}
	return value
}
