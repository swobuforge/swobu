package runtime_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	customprovider "github.com/metrofun/swobu/internal/adapters/outbound/providers/custom"
	"github.com/metrofun/swobu/internal/bootstrap"
	"github.com/metrofun/swobu/internal/domain/protocolsurface"
)

func TestRuntimeOperatorControlPlane_PutGetListDeleteAndServeUpdatedEndpoint(t *testing.T) {
	var gotPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_1","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer upstream.Close()

	configPath := filepath.Join(t.TempDir(), "swobu.yaml")
	if err := os.WriteFile(configPath, []byte(renderRuntimeConfigYAML(nil, "127.0.0.1:0")), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	daemon, err := bootstrap.Start(context.Background(), bootstrap.StartInput{
		ConfigPath: configPath,
		Providers:  customprovider.NewExecutor(http.DefaultClient, staticCredentialResolver{}),
	})
	if err != nil {
		t.Fatalf("bootstrap.Start returned error: %v", err)
	}
	defer func() { _ = daemon.Close(context.Background()) }()

	putBody := endpointDocument{
		Name:                      "beta",
		SelectedProviderConfigRef: "backend-b",
		ProviderConfigs: []providerConfigDocument{{
			Ref:          "backend-b",
			ProviderSpec: "custom",
			BaseURL:      upstream.URL + "/v1",
			ModelID:      "m",
			ProtocolKind: string(protocolsurface.ChatCompletions),
		}},
	}
	raw, err := json.Marshal(putBody)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	req, err := http.NewRequest(http.MethodPut, daemon.BaseURL()+"/_swobu/endpoints/beta", bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do PUT returned error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("PUT status = %d, want 200, body=%s", resp.StatusCode, string(body))
	}

	listResp, err := http.Get(daemon.BaseURL() + "/_swobu/endpoints")
	if err != nil {
		t.Fatalf("GET list returned error: %v", err)
	}
	defer func() { _ = listResp.Body.Close() }()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("GET list status = %d, want 200", listResp.StatusCode)
	}
	var listed struct {
		Endpoints []endpointDocument `json:"endpoints"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&listed); err != nil {
		t.Fatalf("Decode list returned error: %v", err)
	}
	if len(listed.Endpoints) != 1 || listed.Endpoints[0].Name != "beta" {
		t.Fatalf("listed endpoints = %#v, want [beta]", listed.Endpoints)
	}
	savedConfig, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if !bytes.Contains(savedConfig, []byte("name: beta")) {
		t.Fatalf("saved config = %s, want persisted beta endpoint", string(savedConfig))
	}

	requestResp, err := http.Post(daemon.BaseURL()+"/c/beta/chat/completions", "application/json", strings.NewReader(`{"messages":[{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatalf("POST request returned error: %v", err)
	}
	defer func() { _ = requestResp.Body.Close() }()
	if requestResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(requestResp.Body)
		t.Fatalf("request status = %d, want 200, body=%s", requestResp.StatusCode, string(body))
	}
	if gotPath != "/v1/chat/completions" {
		t.Fatalf("upstream path = %q, want %q", gotPath, "/v1/chat/completions")
	}

	deleteReq, err := http.NewRequest(http.MethodDelete, daemon.BaseURL()+"/_swobu/endpoints/beta", nil)
	if err != nil {
		t.Fatalf("NewRequest delete returned error: %v", err)
	}
	deleteResp, err := http.DefaultClient.Do(deleteReq)
	if err != nil {
		t.Fatalf("DELETE returned error: %v", err)
	}
	defer func() { _ = deleteResp.Body.Close() }()
	if deleteResp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE status = %d, want 204", deleteResp.StatusCode)
	}

	getResp, err := http.Get(daemon.BaseURL() + "/_swobu/endpoints/beta")
	if err != nil {
		t.Fatalf("GET resource returned error: %v", err)
	}
	defer func() { _ = getResp.Body.Close() }()
	if getResp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET deleted endpoint status = %d, want 404", getResp.StatusCode)
	}
}

type endpointDocument struct {
	Name                      string                   `json:"name"`
	SelectedProviderConfigRef string                   `json:"selected_provider_config_ref"`
	ProviderConfigs           []providerConfigDocument `json:"provider_configs"`
}

type providerConfigDocument struct {
	Ref          string `json:"ref"`
	ProviderSpec string `json:"provider_spec"`
	BaseURL      string `json:"base_url,omitempty"`
	ModelID      string `json:"model_id,omitempty"`
	ProtocolKind string `json:"protocol_kind"`
}
