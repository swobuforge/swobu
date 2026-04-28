package runtime_test

import (
	"bytes"
	"context"
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

func TestRuntimeBootstrap_LoadsFileBackedEndpointIntentAndServesRequests(t *testing.T) {
	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_1","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer upstream.Close()

	daemon := startDaemon(t, runtimeFixture{
		endpoints: []endpointintent.Endpoint{
			testEndpoint(t, "alpha", "backend-a", testProviderConfig(t, "backend-a", "custom", upstream.URL+"/v1", "cred-1", protocolsurface.ChatCompletions)),
		},
		credentials: map[string]string{"cred-1": "token-123"},
	})
	defer func() { _ = daemon.Close(context.Background()) }()

	req, err := http.NewRequest(http.MethodPost, daemon.BaseURL()+"/c/alpha/chat/completions", bytes.NewBufferString(`{"messages":[{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do returned error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll returned error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", resp.StatusCode, string(raw))
	}
	if !bytes.Contains(raw, []byte(`"content":"ok"`)) {
		t.Fatalf("body = %s, want assistant content", string(raw))
	}
	if gotAuth != "Bearer token-123" {
		t.Fatalf("authorization header = %q, want %q", gotAuth, "Bearer token-123")
	}
}

type runtimeFixture struct {
	endpoints   []endpointintent.Endpoint
	credentials map[string]string
}

func startDaemon(t *testing.T, fixture runtimeFixture) *bootstrap.Daemon {
	t.Helper()

	configPath := filepath.Join(t.TempDir(), "swobu.yaml")
	if err := os.WriteFile(configPath, []byte(renderRuntimeConfigYAML(fixture.endpoints, "127.0.0.1:0")), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	daemon, err := bootstrap.Start(context.Background(), bootstrap.StartInput{
		ConfigPath: configPath,
		Providers:  customprovider.NewExecutor(http.DefaultClient, staticCredentialResolver(fixture.credentials)),
		Continuity: nil,
		Evidence:   nil,
	})
	if err != nil {
		t.Fatalf("bootstrap.Start returned error: %v", err)
	}
	return daemon
}

type staticCredentialResolver map[string]string

func (r staticCredentialResolver) ResolveCredential(_ context.Context, _, credentialRef string) (string, error) {
	return r[credentialRef], nil
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
	providerConfig, err = providerConfig.WithModelID("m")
	if err != nil {
		t.Fatalf("WithModelID returned error: %v", err)
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
