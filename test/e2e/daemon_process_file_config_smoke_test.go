package e2e_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/metrofun/swobu/internal/domain/endpointintent"
	"github.com/metrofun/swobu/internal/domain/protocolsurface"
	"github.com/metrofun/swobu/test/e2e/harness"
)

func TestDaemonProcessFileConfigSmoke_AllowsRealHTTPCall(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("upstream path = %q, want /v1/chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Fatalf("authorization header = %q, want empty", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_1","object":"chat.completion","created":1,"model":"gpt-4.1-mini","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer upstream.Close()

	daemon := harness.StartDaemonProcess(t, harness.DaemonProcessConfig{
		Endpoints: []endpointintent.Endpoint{
			harness.NewEndpoint(
				t,
				"alpha",
				"backend-a",
				mustProviderConfigWithModelID(
					t,
					harness.NewProviderConfig(
						t,
						"backend-a",
						"custom",
						upstream.URL+"/v1",
						"",
						protocolsurface.ChatCompletions,
					),
					"gpt-4.1-mini",
				),
			),
		},
	})

	req, err := http.NewRequest(http.MethodPost, daemon.BaseURL+"/c/alpha/chat/completions", bytes.NewBufferString(`{"messages":[{"role":"user","content":"hi"}]}`))
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

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200, body=%s", resp.StatusCode, string(raw))
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll returned error: %v", err)
	}
	if !bytes.Contains(raw, []byte(`"content":"ok"`)) {
		t.Fatalf("body = %s, want assistant output", string(raw))
	}

	status, exitCode, err := daemon.Status()
	if err != nil {
		t.Fatalf("Status returned error: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("status exit code = %d, want 0", exitCode)
	}
	if status.State != "healthy" {
		t.Fatalf("state = %q, want healthy", status.State)
	}
}
