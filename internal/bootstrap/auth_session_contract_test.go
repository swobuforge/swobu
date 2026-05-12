package bootstrap_test

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/app/operator/authplane"
	operatorclient "github.com/swobuforge/swobu/internal/app/operator/client"
	"github.com/swobuforge/swobu/internal/bootstrap"
)

func TestDaemonAuthSession_StartCallbackPollFailureIsSurfaced(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "swobu.yaml")
	configYAML := `
bind_addr: 127.0.0.1:0
endpoints:
  - name: testname
    selected_provider_config_ref: chatgpt-main
    provider_configs:
      - ref: chatgpt-main
        provider_spec: chatgpt
        protocol_kind: responses
        base_url: http://127.0.0.1:8317/v1
        model_id: gpt-4.1
`
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	daemon, err := bootstrap.Start(context.Background(), bootstrap.StartInput{ConfigPath: configPath})
	if err != nil {
		t.Fatalf("bootstrap.Start returned error: %v", err)
	}
	defer func() { _ = daemon.Close(context.Background()) }()

	client := operatorclient.New(http.DefaultClient, daemon.BaseURL())
	start, err := client.StartAuthSession(
		context.Background(),
		"chatgpt",
		authplane.EncodeEndpointCredentialLocator("testname", "chatgpt-main"),
		"browser",
	)
	if err != nil {
		t.Fatalf("StartAuthSession returned error: %v", err)
	}
	if start.SessionID == "" {
		t.Fatal("empty session id")
	}
	if start.AuthorizeURL == "" {
		t.Fatal("empty authorize url")
	}
	u, err := url.Parse(start.AuthorizeURL)
	if err != nil {
		t.Fatalf("parse authorize url: %v", err)
	}
	state := strings.TrimSpace(u.Query().Get("state"))
	if state == "" {
		t.Fatal("missing oauth state")
	}

	resp, err := http.Get(daemon.BaseURL() + "/_swobu/auth/chatgpt/callback?state=" + url.QueryEscape(state) + "&error=access_denied&request_id=req_contract_1")
	if err != nil {
		t.Fatalf("callback request error: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if !strings.Contains(string(body), "Authentication Error") {
		t.Fatalf("callback body missing auth error title: %q", string(body))
	}
	if !strings.Contains(string(body), "request ID req_contract_1") {
		t.Fatalf("callback body missing request id guidance: %q", string(body))
	}

	status, err := client.GetAuthSessionStatus(context.Background(), start.SessionID)
	if err != nil {
		t.Fatalf("GetAuthSessionStatus returned error: %v", err)
	}
	if status.State != "failed" {
		t.Fatalf("status state = %q, want failed", status.State)
	}
	if !strings.Contains(status.ErrorMessage, "access_denied") {
		t.Fatalf("status error message = %q", status.ErrorMessage)
	}
	if !strings.Contains(status.ErrorMessage, "req_contract_1") {
		t.Fatalf("status error message missing request id = %q", status.ErrorMessage)
	}
}

func TestDaemonAuthSession_CallbackUnknownStateNotFound(t *testing.T) {
	t.Parallel()
	configPath := filepath.Join(t.TempDir(), "swobu.yaml")
	if err := os.WriteFile(configPath, []byte("bind_addr: 127.0.0.1:0\nendpoints: []\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	daemon, err := bootstrap.Start(context.Background(), bootstrap.StartInput{ConfigPath: configPath})
	if err != nil {
		t.Fatalf("bootstrap.Start returned error: %v", err)
	}
	defer func() { _ = daemon.Close(context.Background()) }()

	resp, err := http.Get(daemon.BaseURL() + "/_swobu/auth/chatgpt/callback?state=missing&code=abc")
	if err != nil {
		t.Fatalf("callback request error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status=%d want=%d", resp.StatusCode, http.StatusNotFound)
	}
}
