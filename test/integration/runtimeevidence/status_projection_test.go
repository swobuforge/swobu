package runtimeevidence_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	customprovider "github.com/metrofun/swobu/internal/adapters/outbound/providers/custom"
	"github.com/metrofun/swobu/internal/bootstrap"
	"github.com/metrofun/swobu/internal/domain/endpointintent"
	"github.com/metrofun/swobu/internal/domain/protocolsurface"
)

func TestDaemon_StatusProjection_ReflectsAppendedTraffic(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_1","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer upstream.Close()

	providerConfig := testProviderConfig(t, "backend-a", "custom", upstream.URL+"/v1", "", protocolsurface.ChatCompletions)
	providerConfig, err := providerConfig.WithModelID("m")
	if err != nil {
		t.Fatalf("WithModelID returned error: %v", err)
	}

	daemon := startDaemonWithFixture(t, runtimeFixture{
		endpoints: []endpointintent.Endpoint{
			testEndpoint(t, "alpha", "backend-a", providerConfig),
		},
	})
	defer func() { _ = daemon.Close(context.Background()) }()

	req, err := http.NewRequest(http.MethodPost, daemon.BaseURL()+"/c/alpha/chat/completions", bytes.NewBufferString(`{"messages":[{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Codex/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do returned error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if _, err := io.ReadAll(resp.Body); err != nil {
		t.Fatalf("ReadAll returned error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	projection, err := daemon.StatusProjection()
	if err != nil {
		t.Fatalf("StatusProjection returned error: %v", err)
	}
	if projection.State != string(bootstrap.HealthStateHealthy) {
		t.Fatalf("state = %q, want %q", projection.State, bootstrap.HealthStateHealthy)
	}
	if got := projection.Scope.Kind; got != "all" {
		t.Fatalf("scope kind = %q, want %q", got, "all")
	}
	if projection.Counters.Count2xx != 1 {
		t.Fatalf("2xx count = %d, want 1", projection.Counters.Count2xx)
	}
	if len(projection.RecentTraffic) != 1 {
		t.Fatalf("recent traffic len = %d, want 1", len(projection.RecentTraffic))
	}
	if got := projection.RecentTraffic[0].Route; got != "backend-a" {
		t.Fatalf("recent route = %q, want %q", got, "backend-a")
	}
	if got := projection.RecentTraffic[0].ModelRequested; got != "" {
		t.Fatalf("recent model requested = %q, want empty", got)
	}
	if got := projection.RecentTraffic[0].ModelResolutionMode; got != "default_missing" {
		t.Fatalf("recent model resolution mode = %q, want %q", got, "default_missing")
	}
	if got := projection.RecentTraffic[0].ClientHandler; got != "codex" {
		t.Fatalf("recent client handler = %q, want %q", got, "codex")
	}
	if got := projection.RecentTraffic[0].ClientProtocol; got != "openai_compat" {
		t.Fatalf("recent client protocol = %q, want %q", got, "openai_compat")
	}
	if got := projection.RecentTraffic[0].IngressFamily; got != "chat_completions" {
		t.Fatalf("recent ingress family = %q, want %q", got, "chat_completions")
	}
	if got := projection.RecentTraffic[0].NormalizedOp; got != "/chat/completions" {
		t.Fatalf("recent normalized op = %q, want %q", got, "/chat/completions")
	}

	resp, err = http.Get(daemon.BaseURL() + "/_swobu/status-projection?scope=endpoint:alpha")
	if err != nil {
		t.Fatalf("Get status projection returned error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status projection status = %d, want 200", resp.StatusCode)
	}
	var projectionDoc struct {
		Scope struct {
			Kind     string `json:"kind"`
			Endpoint string `json:"endpoint"`
		} `json:"scope"`
		RecentTraffic []struct {
			RequestID           string `json:"request_id"`
			Route               string `json:"route"`
			ClientHandler       string `json:"client_handler"`
			ClientProtocol      string `json:"client_protocol"`
			IngressFamily       string `json:"ingress_family"`
			NormalizedOp        string `json:"normalized_op"`
			ModelRequested      string `json:"model_requested"`
			ModelResolutionMode string `json:"model_resolution_mode"`
		} `json:"recent_traffic"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&projectionDoc); err != nil {
		t.Fatalf("Decode status projection returned error: %v", err)
	}
	if projectionDoc.Scope.Kind != "endpoint" || projectionDoc.Scope.Endpoint != "alpha" {
		t.Fatalf("status projection scope = %#v, want endpoint alpha", projectionDoc.Scope)
	}
	if len(projectionDoc.RecentTraffic) != 1 || projectionDoc.RecentTraffic[0].Route != "backend-a" {
		t.Fatalf("status projection doc = %#v, want one backend-a row", projectionDoc)
	}
	if got := projectionDoc.RecentTraffic[0].ModelRequested; got != "" {
		t.Fatalf("status projection model requested = %q, want empty", got)
	}
	if got := projectionDoc.RecentTraffic[0].ModelResolutionMode; got != "default_missing" {
		t.Fatalf("status projection model resolution mode = %q, want %q", got, "default_missing")
	}
	if got := projectionDoc.RecentTraffic[0].ClientHandler; got != "codex" {
		t.Fatalf("status projection client handler = %q, want %q", got, "codex")
	}
}

type runtimeFixture struct {
	endpoints []endpointintent.Endpoint
}

func startDaemonWithFixture(t *testing.T, fixture runtimeFixture) *bootstrap.Daemon {
	t.Helper()

	configPath := filepath.Join(t.TempDir(), "swobu.yaml")
	if err := os.WriteFile(configPath, []byte(renderRuntimeConfigYAML(fixture.endpoints, "127.0.0.1:0")), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	daemon, err := bootstrap.Start(context.Background(), bootstrap.StartInput{
		ConfigPath: configPath,
		Providers:  customprovider.NewExecutor(http.DefaultClient, nil),
	})
	if err != nil {
		t.Fatalf("bootstrap.Start returned error: %v", err)
	}
	return daemon
}

func testEndpoint(t *testing.T, name string, selectedRef string, providerConfigs ...endpointintent.ProviderConfig) endpointintent.Endpoint {
	t.Helper()

	parsedName, err := endpointintent.ParseEndpointName(name)
	if err != nil {
		t.Fatalf("ParseEndpointName returned error: %v", err)
	}
	selected, err := endpointintent.ParseProviderConfigRef(selectedRef)
	if err != nil {
		t.Fatalf("ParseProviderConfigRef returned error: %v", err)
	}
	endpoint, err := endpointintent.NewEndpoint(parsedName, providerConfigs, selected)
	if err != nil {
		t.Fatalf("NewEndpoint returned error: %v", err)
	}
	return endpoint
}

func testProviderConfig(t *testing.T, ref string, providerSpec string, baseURL string, credentialRef string, protocol protocolsurface.Kind) endpointintent.ProviderConfig {
	t.Helper()

	parsedRef, err := endpointintent.ParseProviderConfigRef(ref)
	if err != nil {
		t.Fatalf("ParseProviderConfigRef returned error: %v", err)
	}
	parsedSpec, err := endpointintent.ParseProviderSpec(providerSpec)
	if err != nil {
		t.Fatalf("ParseProviderSpec returned error: %v", err)
	}
	providerConfig, err := endpointintent.NewProviderConfig(parsedRef, parsedSpec, baseURL, credentialRef, protocol)
	if err != nil {
		t.Fatalf("NewProviderConfig returned error: %v", err)
	}
	return providerConfig
}

func renderRuntimeConfigYAML(endpoints []endpointintent.Endpoint, bindAddr string) string {
	var b strings.Builder
	b.WriteString("bind_addr: ")
	b.WriteString(bindAddr)
	b.WriteString("\n")
	b.WriteString("endpoints:\n")
	if len(endpoints) == 0 {
		b.WriteString("  []\n")
		return b.String()
	}
	for _, endpoint := range endpoints {
		fmt.Fprintf(&b, "  - name: %s\n", endpoint.Name().String())
		fmt.Fprintf(&b, "    selected_provider_config_ref: %s\n", endpoint.SelectedProviderConfigRef().String())
		b.WriteString("    provider_configs:\n")
		for _, providerConfig := range endpoint.ProviderConfigs() {
			fmt.Fprintf(&b, "      - ref: %s\n", providerConfig.Ref().String())
			fmt.Fprintf(&b, "        provider_spec: %s\n", providerConfig.ProviderSpec().String())
			fmt.Fprintf(&b, "        protocol_kind: %s\n", providerConfig.ProtocolKind().String())
			if providerConfig.BaseURL() != "" {
				fmt.Fprintf(&b, "        base_url: %s\n", providerConfig.BaseURL())
			}
			if providerConfig.CredentialRef() != "" {
				fmt.Fprintf(&b, "        credential_ref: %s\n", providerConfig.CredentialRef())
			}
			if providerConfig.ModelID() != "" {
				fmt.Fprintf(&b, "        model_id: %s\n", providerConfig.ModelID())
			}
		}
	}
	return b.String()
}
