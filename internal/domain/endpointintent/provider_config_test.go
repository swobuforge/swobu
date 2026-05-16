package endpointintent

import (
	"errors"
	"testing"
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
	if _, err := cfg.WithTargetAlias("primary"); !errors.Is(err, ErrInvalidProviderConfig) {
		t.Fatalf("WithTargetAlias(primary) error = %v, want ErrInvalidProviderConfig", err)
	}
	if _, err := cfg.WithTargetAlias("gpt.5"); !errors.Is(err, ErrInvalidProviderConfig) {
		t.Fatalf("WithTargetAlias(gpt.5) error = %v, want ErrInvalidProviderConfig", err)
	}
}
