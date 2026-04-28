package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/metrofun/swobu/internal/domain/endpointintent"
	"github.com/metrofun/swobu/internal/domain/protocolsurface"
)

func TestLoad_AppliesDefaultsAndDecodesEndpoints(t *testing.T) {
	path := filepath.Join(t.TempDir(), "swobu.yaml")
	raw := `
endpoints:
  - name: alpha
    selected_provider_config_ref: backend-a
    provider_configs:
      - ref: backend-a
        provider_spec: custom
        protocol_kind: chat_completions
        base_url: https://example.test/v1
        model_id: gpt-4.1-mini
        target_alias: fast
`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if got := loaded.Runtime.BindAddr; got != DefaultBindAddr() {
		t.Fatalf("bind addr = %q, want %q", got, DefaultBindAddr())
	}
	if len(loaded.Endpoints) != 1 {
		t.Fatalf("endpoint count = %d, want 1", len(loaded.Endpoints))
	}
	if got := loaded.Endpoints[0].Name().String(); got != "alpha" {
		t.Fatalf("endpoint name = %q, want alpha", got)
	}
	selectedProvider := loaded.Endpoints[0].SelectedProviderConfig()
	if got := selectedProvider.ModelID(); got != "gpt-4.1-mini" {
		t.Fatalf("selected provider model_id = %q, want %q", got, "gpt-4.1-mini")
	}
	if got := selectedProvider.TargetAlias(); got != "fast" {
		t.Fatalf("selected provider target_alias = %q, want %q", got, "fast")
	}
}

func TestLoad_RejectsInvalidConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "swobu.yaml")
	raw := `
endpoints:
  - name: Alpha
    selected_provider_config_ref: missing
    provider_configs: []
`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid endpoint name") {
		t.Fatalf("error = %v, want invalid endpoint name", err)
	}
}

func TestLoad_RejectsCustomProviderConfigWithoutBaseURL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "swobu.yaml")
	raw := `
endpoints:
  - name: alpha
    selected_provider_config_ref: backend-a
    provider_configs:
      - ref: backend-a
        provider_spec: custom
        protocol_kind: chat_completions
`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "base_url") {
		t.Fatalf("error = %v, want base_url validation failure", err)
	}
}

func TestSave_PersistsProviderModelID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "swobu.yaml")
	name, err := endpointintent.ParseEndpointName("alpha")
	if err != nil {
		t.Fatalf("ParseEndpointName returned error: %v", err)
	}
	ref, err := endpointintent.ParseProviderConfigRef("backend-a")
	if err != nil {
		t.Fatalf("ParseProviderConfigRef returned error: %v", err)
	}
	spec, err := endpointintent.ParseProviderSpec("custom")
	if err != nil {
		t.Fatalf("ParseProviderSpec returned error: %v", err)
	}
	providerConfig, err := endpointintent.NewProviderConfig(
		ref,
		spec,
		"https://example.test/v1",
		"",
		protocolsurface.ChatCompletions,
	)
	if err != nil {
		t.Fatalf("NewProviderConfig returned error: %v", err)
	}
	providerConfig, err = providerConfig.WithModelID("gpt-4.1-mini")
	if err != nil {
		t.Fatalf("WithModelID returned error: %v", err)
	}
	providerConfig, err = providerConfig.WithTargetAlias("fast")
	if err != nil {
		t.Fatalf("WithTargetAlias returned error: %v", err)
	}
	endpoint, err := endpointintent.NewEndpoint(name, []endpointintent.ProviderConfig{providerConfig}, ref)
	if err != nil {
		t.Fatalf("NewEndpoint returned error: %v", err)
	}

	if err := Save(path, RuntimeConfig{BindAddr: "127.0.0.1:7777"}, []endpointintent.Endpoint{endpoint}); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	text := string(raw)
	if !strings.Contains(text, "model_id: gpt-4.1-mini") {
		t.Fatalf("saved config missing model_id, got:\n%s", text)
	}
	if !strings.Contains(text, "target_alias: fast") {
		t.Fatalf("saved config missing target_alias, got:\n%s", text)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(loaded.Endpoints) != 1 {
		t.Fatalf("endpoint count = %d, want 1", len(loaded.Endpoints))
	}
	if got := loaded.Endpoints[0].SelectedProviderConfig().ModelID(); got != "gpt-4.1-mini" {
		t.Fatalf("roundtrip model_id = %q, want %q", got, "gpt-4.1-mini")
	}
	if got := loaded.Endpoints[0].SelectedProviderConfig().TargetAlias(); got != "fast" {
		t.Fatalf("roundtrip target_alias = %q, want %q", got, "fast")
	}
}
