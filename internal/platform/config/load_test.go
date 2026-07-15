package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/endpointintent"
)

func TestLoad_AppliesDefaultsAndDecodesEndpoints(t *testing.T) {
	path := filepath.Join(t.TempDir(), "swobu.yaml")
	raw := `
endpoints:
  - name: alpha
    selected_provider_config_ref: backend-a
    provider_configs:
      - ref: backend-a
        provider_spec: openai_compatible
        base_url: https://example.test/v1
        model_id: gpt-4.1-mini
        target_alias: fast
        target_rank: 2
        target_weight: 4
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
	if got := loaded.Runtime.PatchDiagnosticThresholdsConfig.MinRepeatedDecodeMutations; got != 2 {
		t.Fatalf("min_repeated_decode_mutations = %d, want 2", got)
	}
	if got := loaded.Runtime.PatchDiagnosticThresholdsConfig.MinNoopRatioPopulation; got != 3 {
		t.Fatalf("min_noop_ratio_population = %d, want 3", got)
	}
	if got := loaded.Runtime.PatchDiagnosticThresholdsConfig.NoopRatioPercentThreshold; got != 80 {
		t.Fatalf("noop_ratio_percent_threshold = %d, want 80", got)
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
	if got := selectedProvider.TargetRank(); got != 2 {
		t.Fatalf("selected provider target_rank = %d, want 2", got)
	}
	if got := selectedProvider.TargetWeight(); got != 4 {
		t.Fatalf("selected provider target_weight = %d, want 4", got)
	}
}

func TestLoad_OverridesPatchDiagnosticThresholdsConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "swobu.yaml")
	raw := `
patch_diagnostic_thresholds:
  min_repeated_decode_mutations: 4
  min_noop_ratio_population: 6
  noop_ratio_percent_threshold: 70
endpoints:
  - name: alpha
    selected_provider_config_ref: backend-a
    provider_configs:
      - ref: backend-a
        provider_spec: openai_compatible
        base_url: https://example.test/v1
`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if got := loaded.Runtime.PatchDiagnosticThresholdsConfig.MinRepeatedDecodeMutations; got != 4 {
		t.Fatalf("min_repeated_decode_mutations = %d, want 4", got)
	}
	if got := loaded.Runtime.PatchDiagnosticThresholdsConfig.MinNoopRatioPopulation; got != 6 {
		t.Fatalf("min_noop_ratio_population = %d, want 6", got)
	}
	if got := loaded.Runtime.PatchDiagnosticThresholdsConfig.NoopRatioPercentThreshold; got != 70 {
		t.Fatalf("noop_ratio_percent_threshold = %d, want 70", got)
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
        provider_spec: openai_compatible
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

func TestLoad_RejectsProtocolAutoInPersistedConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "swobu.yaml")
	raw := `
endpoints:
  - name: alpha
    selected_provider_config_ref: backend-a
    provider_configs:
      - ref: backend-a
        provider_spec: openai
        provider_protocol: auto
        credential_ref: env:OPENAI_API_KEY
`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "auto") {
		t.Fatalf("error = %v, want auto validation failure", err)
	}
}

func TestLoad_PreservesProviderProtocol(t *testing.T) {
	path := filepath.Join(t.TempDir(), "swobu.yaml")
	raw := `
endpoints:
  - name: jobs
    selected_provider_config_ref: cfg-main
    provider_configs:
      - ref: cfg-main
        provider_spec: openai
        base_url: https://api.openai.com/v1
        credential_ref: env:OPENAI_API_KEY
        provider_protocol: responses_stream
        model_id: gpt-5.4-mini
`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(loaded.Endpoints) != 1 {
		t.Fatalf("endpoint count=%d want 1", len(loaded.Endpoints))
	}
	providers := loaded.Endpoints[0].ProviderConfigs()
	if len(providers) != 1 {
		t.Fatalf("provider config count=%d want 1", len(providers))
	}
	if got := providers[0].ProviderProtocol(); got != "responses_stream" {
		t.Fatalf("provider protocol=%q want=%q", got, "responses_stream")
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
	spec, err := endpointintent.ParseProviderSpec("openai_compatible")
	if err != nil {
		t.Fatalf("ParseProviderSpec returned error: %v", err)
	}
	providerConfig, err := endpointintent.NewProviderConfig(
		ref,
		spec,
		"https://example.test/v1",
		"",
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
	providerConfig, err = providerConfig.WithTargetRank(2)
	if err != nil {
		t.Fatalf("WithTargetRank returned error: %v", err)
	}
	providerConfig, err = providerConfig.WithTargetWeight(4)
	if err != nil {
		t.Fatalf("WithTargetWeight returned error: %v", err)
	}
	endpoint, err := endpointintent.NewEndpoint(name, []endpointintent.ProviderConfig{providerConfig}, ref)
	if err != nil {
		t.Fatalf("NewEndpoint returned error: %v", err)
	}

	if err := Save(path, RuntimeConfig{BindAddr: "127.0.0.1:7926"}, []endpointintent.Endpoint{endpoint}); err != nil {
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
	if !strings.Contains(text, "target_rank: 2") {
		t.Fatalf("saved config missing target_rank, got:\n%s", text)
	}
	if !strings.Contains(text, "target_weight: 4") {
		t.Fatalf("saved config missing target_weight, got:\n%s", text)
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
	if got := loaded.Endpoints[0].SelectedProviderConfig().TargetRank(); got != 2 {
		t.Fatalf("roundtrip target_rank = %d, want 2", got)
	}
	if got := loaded.Endpoints[0].SelectedProviderConfig().TargetWeight(); got != 4 {
		t.Fatalf("roundtrip target_weight = %d, want 4", got)
	}
}

func TestSaveLoad_RoundTripMultiProviderCredentialRefsAndAliases_YAML(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "swobu.yaml")
	raw := `
bind_addr: 127.0.0.1:7926
endpoints:
  - name: acme
    selected_provider_config_ref: chatgpt-work
    provider_configs:
      - ref: chatgpt-work
        provider_spec: chatgpt
        credential_ref: keychain:chatgpt/work_account
        model_id: gpt-5.3-codex
        target_alias: work-fast
      - ref: chatgpt-personal
        provider_spec: chatgpt
        credential_ref: keychain:chatgpt/personal_account
        model_id: gpt-5.3-codex
        target_alias: personal-safe
`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(loaded.Endpoints) != 1 {
		t.Fatalf("endpoint count=%d want 1", len(loaded.Endpoints))
	}
	cfgs := loaded.Endpoints[0].ProviderConfigs()
	if len(cfgs) != 2 {
		t.Fatalf("provider config count=%d want 2", len(cfgs))
	}
	if got := cfgs[0].CredentialRef(); got != "keychain:chatgpt/work_account" {
		t.Fatalf("credential_ref[0]=%q", got)
	}
	if got := cfgs[1].CredentialRef(); got != "keychain:chatgpt/personal_account" {
		t.Fatalf("credential_ref[1]=%q", got)
	}
	if got := cfgs[0].TargetAlias(); got != "work-fast" {
		t.Fatalf("target_alias[0]=%q", got)
	}
	if got := cfgs[1].TargetAlias(); got != "personal-safe" {
		t.Fatalf("target_alias[1]=%q", got)
	}
}

func TestSaveLoad_RoundTripMultiProviderCredentialRefsAndAliases_JSON(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "swobu.json")
	raw := `{
  "bind_addr": "127.0.0.1:7926",
  "endpoints": [
    {
      "name": "acme",
      "selected_provider_config_ref": "chatgpt-work",
      "provider_configs": [
        {
          "ref": "chatgpt-work",
          "provider_spec": "chatgpt",
          "credential_ref": "keychain:chatgpt/work_account",
          "model_id": "gpt-5.3-codex",
          "target_alias": "work-fast"
        },
        {
          "ref": "chatgpt-personal",
          "provider_spec": "chatgpt",
          "credential_ref": "keychain:chatgpt/personal_account",
          "model_id": "gpt-5.3-codex",
          "target_alias": "personal-safe"
        }
      ]
    }
  ]
}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if err := Save(path, loaded.Runtime, loaded.Endpoints); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}
	roundtrip, err := Load(path)
	if err != nil {
		t.Fatalf("Load roundtrip returned error: %v", err)
	}
	cfgs := roundtrip.Endpoints[0].ProviderConfigs()
	if len(cfgs) != 2 {
		t.Fatalf("provider config count=%d want 2", len(cfgs))
	}
	if got := cfgs[0].CredentialRef(); got != "keychain:chatgpt/work_account" {
		t.Fatalf("roundtrip credential_ref[0]=%q", got)
	}
	if got := cfgs[1].CredentialRef(); got != "keychain:chatgpt/personal_account" {
		t.Fatalf("roundtrip credential_ref[1]=%q", got)
	}
	if got := cfgs[0].TargetAlias(); got != "work-fast" {
		t.Fatalf("roundtrip target_alias[0]=%q", got)
	}
	if got := cfgs[1].TargetAlias(); got != "personal-safe" {
		t.Fatalf("roundtrip target_alias[1]=%q", got)
	}
}

func TestSave_OmitsDefaultRankWeight(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "swobu.yaml")
	name, err := endpointintent.ParseEndpointName("jobs")
	if err != nil {
		t.Fatalf("ParseEndpointName returned error: %v", err)
	}
	ref, err := endpointintent.ParseProviderConfigRef("cfg-main")
	if err != nil {
		t.Fatalf("ParseProviderConfigRef returned error: %v", err)
	}
	spec, err := endpointintent.ParseProviderSpec("openai")
	if err != nil {
		t.Fatalf("ParseProviderSpec returned error: %v", err)
	}
	cfg, err := endpointintent.NewProviderConfig(ref, spec, "https://api.openai.com/v1", "env:OPENAI_API_KEY")
	if err != nil {
		t.Fatalf("NewProviderConfig returned error: %v", err)
	}
	if cfg.TargetRank() != 1 || cfg.TargetWeight() != 1 {
		t.Fatalf("defaults must be 1/1, got %d/%d", cfg.TargetRank(), cfg.TargetWeight())
	}
	endpoint, err := endpointintent.NewEndpoint(name, []endpointintent.ProviderConfig{cfg}, ref)
	if err != nil {
		t.Fatalf("NewEndpoint returned error: %v", err)
	}

	if err := Save(path, RuntimeConfig{BindAddr: DefaultBindAddr()}, []endpointintent.Endpoint{endpoint}); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	text := string(raw)
	if strings.Contains(text, "target_rank:") {
		t.Fatalf("saved config should omit target_rank for default value 1, got:\n%s", text)
	}
	if strings.Contains(text, "target_weight:") {
		t.Fatalf("saved config should omit target_weight for default value 1, got:\n%s", text)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if got := loaded.Endpoints[0].ProviderConfigs()[0].TargetRank(); got != 1 {
		t.Fatalf("roundtrip target_rank = %d, want 1", got)
	}
	if got := loaded.Endpoints[0].ProviderConfigs()[0].TargetWeight(); got != 1 {
		t.Fatalf("roundtrip target_weight = %d, want 1", got)
	}
}

func TestSave_PersistsProviderProtocol(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "swobu.yaml")
	name, err := endpointintent.ParseEndpointName("jobs")
	if err != nil {
		t.Fatalf("ParseEndpointName returned error: %v", err)
	}
	ref, err := endpointintent.ParseProviderConfigRef("cfg-main")
	if err != nil {
		t.Fatalf("ParseProviderConfigRef returned error: %v", err)
	}
	spec, err := endpointintent.ParseProviderSpec("openai")
	if err != nil {
		t.Fatalf("ParseProviderSpec returned error: %v", err)
	}
	cfg, err := endpointintent.NewProviderConfig(ref, spec, "https://api.openai.com/v1", "env:OPENAI_API_KEY")
	if err != nil {
		t.Fatalf("NewProviderConfig returned error: %v", err)
	}
	cfg, err = cfg.WithProviderProtocol("responses_stream")
	if err != nil {
		t.Fatalf("WithProviderProtocol returned error: %v", err)
	}
	endpoint, err := endpointintent.NewEndpoint(name, []endpointintent.ProviderConfig{cfg}, ref)
	if err != nil {
		t.Fatalf("NewEndpoint returned error: %v", err)
	}

	if err := Save(path, RuntimeConfig{BindAddr: DefaultBindAddr()}, []endpointintent.Endpoint{endpoint}); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if !strings.Contains(string(raw), "provider_protocol: responses_stream") {
		t.Fatalf("saved config missing provider_protocol, got:\n%s", string(raw))
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	got := loaded.Endpoints[0].ProviderConfigs()[0].ProviderProtocol()
	if got != "responses_stream" {
		t.Fatalf("roundtrip provider protocol=%q want=%q", got, "responses_stream")
	}
}
