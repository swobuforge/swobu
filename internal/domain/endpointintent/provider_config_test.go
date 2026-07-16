package endpointintent

import (
	"errors"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
)

func TestProviderConfig_RequiresExplicitRef(t *testing.T) {
	spec, err := ParseProviderSpec("openai_compatible")
	if err != nil {
		t.Fatalf("ParseProviderSpec returned error: %v", err)
	}

	_, err = NewProviderConfig(
		ProviderConfigRef{},
		spec,
		"https://example.test/v1",
		"cred-1",
	)
	if !errors.Is(err, ErrInvalidProviderConfig) {
		t.Fatalf("expected ErrInvalidProviderConfig, got %v", err)
	}
}

func TestProviderConfig_RejectsIncompleteCustomConfig(t *testing.T) {
	ref, err := ParseProviderConfigRef("cfg-a")
	if err != nil {
		t.Fatalf("ParseProviderConfigRef returned error: %v", err)
	}
	spec, err := ParseProviderSpec("openai_compatible")
	if err != nil {
		t.Fatalf("ParseProviderSpec returned error: %v", err)
	}

	_, err = NewProviderConfig(
		ref,
		spec,
		"",
		"cred-1",
	)
	if !errors.Is(err, ErrInvalidProviderConfig) {
		t.Fatalf("expected ErrInvalidProviderConfig, got %v", err)
	}
}

func TestProviderSpec_RejectsUnknownProviderSpec(t *testing.T) {
	_, err := ParseProviderSpec("unknown-provider")
	if !errors.Is(err, ErrInvalidProviderSpec) {
		t.Fatalf("expected ErrInvalidProviderSpec, got %v", err)
	}
}

func TestProviderSpec_RejectsClaudeAlias(t *testing.T) {
	_, err := ParseProviderSpec("claude")
	if !errors.Is(err, ErrInvalidProviderSpec) {
		t.Fatalf("expected ErrInvalidProviderSpec, got %v", err)
	}
}

func TestProviderConfig_TargetAliasValidation(t *testing.T) {
	ref, err := ParseProviderConfigRef("cfg-a")
	if err != nil {
		t.Fatalf("ParseProviderConfigRef returned error: %v", err)
	}
	spec, err := ParseProviderSpec("openai_compatible")
	if err != nil {
		t.Fatalf("ParseProviderSpec returned error: %v", err)
	}
	cfg, err := NewProviderConfig(ref, spec, "https://example.test/v1", "cred-1")
	if err != nil {
		t.Fatalf("NewProviderConfig returned error: %v", err)
	}

	cfg, err = cfg.WithTargetAlias("FAST")
	if err != nil {
		t.Fatalf("WithTargetAlias returned error: %v", err)
	}
	if got := cfg.TargetAlias(); got != "fast" {
		t.Fatalf("target alias = %q, want %q", got, "fast")
	}
	if _, err := cfg.WithTargetAlias("default"); !errors.Is(err, ErrInvalidProviderConfig) {
		t.Fatalf("WithTargetAlias(default) error = %v, want ErrInvalidProviderConfig", err)
	}
	if _, err := cfg.WithTargetAlias("gpt.5"); !errors.Is(err, ErrInvalidProviderConfig) {
		t.Fatalf("WithTargetAlias(gpt.5) error = %v, want ErrInvalidProviderConfig", err)
	}
}

func TestProviderConfig_TargetRankWeightValidation(t *testing.T) {
	ref, err := ParseProviderConfigRef("cfg-a")
	if err != nil {
		t.Fatalf("ParseProviderConfigRef returned error: %v", err)
	}
	spec, err := ParseProviderSpec("openai_compatible")
	if err != nil {
		t.Fatalf("ParseProviderSpec returned error: %v", err)
	}
	cfg, err := NewProviderConfig(ref, spec, "https://example.test/v1", "cred-1")
	if err != nil {
		t.Fatalf("NewProviderConfig returned error: %v", err)
	}
	if cfg.TargetRank() != 1 || cfg.TargetWeight() != 1 {
		t.Fatalf("defaults rank/weight = %d/%d, want 1/1", cfg.TargetRank(), cfg.TargetWeight())
	}
	cfg, err = cfg.WithTargetRank(2)
	if err != nil {
		t.Fatalf("WithTargetRank returned error: %v", err)
	}
	cfg, err = cfg.WithTargetWeight(7)
	if err != nil {
		t.Fatalf("WithTargetWeight returned error: %v", err)
	}
	if cfg.TargetRank() != 2 || cfg.TargetWeight() != 7 {
		t.Fatalf("rank/weight = %d/%d, want 2/7", cfg.TargetRank(), cfg.TargetWeight())
	}
	if _, err := cfg.WithTargetRank(0); !errors.Is(err, ErrInvalidProviderConfig) {
		t.Fatalf("WithTargetRank(0) error = %v, want ErrInvalidProviderConfig", err)
	}
	if _, err := cfg.WithTargetWeight(0); !errors.Is(err, ErrInvalidProviderConfig) {
		t.Fatalf("WithTargetWeight(0) error = %v, want ErrInvalidProviderConfig", err)
	}
}

func TestProviderConfig_RouteModelIDProjection(t *testing.T) {
	ref, err := ParseProviderConfigRef("cfg-a")
	if err != nil {
		t.Fatalf("ParseProviderConfigRef returned error: %v", err)
	}
	spec, err := ParseProviderSpec("openai_compatible")
	if err != nil {
		t.Fatalf("ParseProviderSpec returned error: %v", err)
	}
	cfg, err := NewProviderConfig(ref, spec, "https://example.test/v1", "cred-1")
	if err != nil {
		t.Fatalf("NewProviderConfig returned error: %v", err)
	}
	cfg, err = cfg.WithModelID("gpt-4.1")
	if err != nil {
		t.Fatalf("WithModelID returned error: %v", err)
	}
	if got, want := cfg.RouteModelID(), "gpt-4.1"; got != want {
		t.Fatalf("route model fallback = %q, want %q", got, want)
	}
	if got, want := projectedRouteModelID(cfg), "gpt-4.1"; got != want {
		t.Fatalf("projected route model = %q, want %q", got, want)
	}
	cfg, err = cfg.WithRouteModelID("  gpt  ")
	if err != nil {
		t.Fatalf("WithRouteModelID returned error: %v", err)
	}
	if got, want := cfg.RouteModelID(), "gpt"; got != want {
		t.Fatalf("route model id = %q, want %q", got, want)
	}
	if got, want := projectedRouteModelID(cfg), "gpt"; got != want {
		t.Fatalf("projected route model = %q, want %q", got, want)
	}
}

func TestProviderConfig_DerivesProtocolFromProviderSpec(t *testing.T) {
	ref, err := ParseProviderConfigRef("cfg-anthropic")
	if err != nil {
		t.Fatalf("ParseProviderConfigRef returned error: %v", err)
	}
	spec, err := ParseProviderSpec("anthropic")
	if err != nil {
		t.Fatalf("ParseProviderSpec returned error: %v", err)
	}
	cfg, err := NewProviderConfig(ref, spec, "https://api.anthropic.com/v1", "cred-1")
	if err != nil {
		t.Fatalf("NewProviderConfig returned error: %v", err)
	}
	if got := cfg.ProtocolKind(); got != protocolkind.Messages {
		t.Fatalf("protocol kind = %q, want %q", got, protocolkind.Messages)
	}
}

func TestNormalizeAzureResourceLocator(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"contact-8837-resource":                               "https://contact-8837-resource.services.ai.azure.com",
		"contact-8837-resource.services.ai.azure.com":         "https://contact-8837-resource.services.ai.azure.com",
		"https://contact-8837-resource.services.ai.azure.com": "https://contact-8837-resource.services.ai.azure.com",
		"https://portal.azure.com/#@tenant/resource/subscriptions/123/resourceGroups/rg/providers/Microsoft.CognitiveServices/accounts/contact-8837-resource": "https://contact-8837-resource.services.ai.azure.com",
	}
	for raw, want := range cases {
		got, err := NormalizeAzureResourceLocator(raw)
		if err != nil {
			t.Fatalf("NormalizeAzureResourceLocator(%q) returned error: %v", raw, err)
		}
		if got != want {
			t.Fatalf("NormalizeAzureResourceLocator(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestProviderConfig_NormalizesAzureResourceLocator(t *testing.T) {
	ref, err := ParseProviderConfigRef("cfg-azure")
	if err != nil {
		t.Fatalf("ParseProviderConfigRef returned error: %v", err)
	}
	spec, err := ParseProviderSpec("azure")
	if err != nil {
		t.Fatalf("ParseProviderSpec returned error: %v", err)
	}
	cfg, err := NewProviderConfig(ref, spec, "contact-8837-resource", "cred-1")
	if err != nil {
		t.Fatalf("NewProviderConfig returned error: %v", err)
	}
	if got := cfg.BaseURL(); got != "https://contact-8837-resource.services.ai.azure.com" {
		t.Fatalf("base URL = %q, want normalized resource locator", got)
	}
	if got := cfg.ProviderProtocol(); got != profile.ProviderProtocolAuto {
		t.Fatalf("provider protocol = %q, want auto", got)
	}
}

func TestProviderConfig_WithProviderProtocol(t *testing.T) {
	ref, err := ParseProviderConfigRef("cfg-openai")
	if err != nil {
		t.Fatalf("ParseProviderConfigRef returned error: %v", err)
	}
	spec, err := ParseProviderSpec("openai_compatible")
	if err != nil {
		t.Fatalf("ParseProviderSpec returned error: %v", err)
	}
	cfg, err := NewProviderConfig(ref, spec, "https://example.test/v1", "cred-1")
	if err != nil {
		t.Fatalf("NewProviderConfig returned error: %v", err)
	}

	cfg, err = cfg.WithProviderProtocol("responses")
	if err != nil {
		t.Fatalf("WithProviderProtocol returned error: %v", err)
	}
	if got := cfg.ProtocolKind(); got != protocolkind.Responses {
		t.Fatalf("protocol kind = %q, want %q", got, protocolkind.Responses)
	}
	if got := cfg.ProviderProtocol(); got != "responses" {
		t.Fatalf("provider protocol = %q, want responses", got)
	}
}

func TestProviderConfig_WithProviderProtocolRejectsUnsupportedProtocol(t *testing.T) {
	ref, err := ParseProviderConfigRef("cfg-anthropic")
	if err != nil {
		t.Fatalf("ParseProviderConfigRef returned error: %v", err)
	}
	spec, err := ParseProviderSpec("anthropic")
	if err != nil {
		t.Fatalf("ParseProviderSpec returned error: %v", err)
	}
	cfg, err := NewProviderConfig(ref, spec, "https://api.anthropic.com/v1", "cred-1")
	if err != nil {
		t.Fatalf("NewProviderConfig returned error: %v", err)
	}
	if _, err := cfg.WithProviderProtocol("completions"); !errors.Is(err, ErrInvalidProviderConfig) {
		t.Fatalf("WithProviderProtocol(completions) error = %v, want ErrInvalidProviderConfig", err)
	}
}

func TestProviderConfig_AuthHeaderDefaultsAndValidation(t *testing.T) {
	ref, err := ParseProviderConfigRef("cfg-openai-compatible")
	if err != nil {
		t.Fatalf("ParseProviderConfigRef returned error: %v", err)
	}
	spec, err := ParseProviderSpec("openai_compatible")
	if err != nil {
		t.Fatalf("ParseProviderSpec returned error: %v", err)
	}
	cfg, err := NewProviderConfig(ref, spec, "https://example.test/v1", "cred-1")
	if err != nil {
		t.Fatalf("NewProviderConfig returned error: %v", err)
	}
	if got := cfg.AuthHeader(); got != "Authorization" {
		t.Fatalf("default auth header = %q, want Authorization", got)
	}

	cfg, err = cfg.WithAuthHeader("X-Custom-Token")
	if err != nil {
		t.Fatalf("WithAuthHeader returned error: %v", err)
	}
	if got := cfg.AuthHeader(); got != "X-Custom-Token" {
		t.Fatalf("custom auth header = %q, want X-Custom-Token", got)
	}
	if _, err := cfg.WithAuthHeader("bad header"); !errors.Is(err, ErrInvalidProviderConfig) {
		t.Fatalf("WithAuthHeader(bad header) error = %v, want ErrInvalidProviderConfig", err)
	}

	openAIRef, err := ParseProviderConfigRef("cfg-openai")
	if err != nil {
		t.Fatalf("ParseProviderConfigRef returned error: %v", err)
	}
	openAISpec, err := ParseProviderSpec("openai")
	if err != nil {
		t.Fatalf("ParseProviderSpec returned error: %v", err)
	}
	openAIConfig, err := NewProviderConfig(openAIRef, openAISpec, "https://api.openai.com/v1", "cred-1")
	if err != nil {
		t.Fatalf("NewProviderConfig returned error: %v", err)
	}
	if _, err := openAIConfig.WithAuthHeader("X-API-Key"); !errors.Is(err, ErrInvalidProviderConfig) {
		t.Fatalf("WithAuthHeader on openai provider error = %v, want ErrInvalidProviderConfig", err)
	}
}
