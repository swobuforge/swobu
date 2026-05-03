package endpointintent

import (
	"errors"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/compatibility"
)

func TestProviderConfig_RequiresExplicitRef(t *testing.T) {
	spec, err := ParseProviderSpec("custom")
	if err != nil {
		t.Fatalf("ParseProviderSpec returned error: %v", err)
	}

	_, err = NewProviderConfig(
		ProviderConfigRef{},
		spec,
		"https://example.test/v1",
		"cred-1",
		"chat_completions",
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
	spec, err := ParseProviderSpec("custom")
	if err != nil {
		t.Fatalf("ParseProviderSpec returned error: %v", err)
	}

	_, err = NewProviderConfig(
		ref,
		spec,
		"",
		"cred-1",
		"chat_completions",
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

func TestProviderConfig_RejectsUnsupportedProviderProtocolBinding(t *testing.T) {
	ref, err := ParseProviderConfigRef("cfg-a")
	if err != nil {
		t.Fatalf("ParseProviderConfigRef returned error: %v", err)
	}
	spec, err := ParseProviderSpec("custom")
	if err != nil {
		t.Fatalf("ParseProviderSpec returned error: %v", err)
	}

	_, err = NewProviderConfig(
		ref,
		spec,
		"https://example.test/v1",
		"cred-1",
		"messages",
	)
	if !errors.Is(err, ErrInvalidProviderConfig) {
		t.Fatalf("expected ErrInvalidProviderConfig, got %v", err)
	}
}

func TestProviderConfig_TargetAliasValidation(t *testing.T) {
	ref, err := ParseProviderConfigRef("cfg-a")
	if err != nil {
		t.Fatalf("ParseProviderConfigRef returned error: %v", err)
	}
	spec, err := ParseProviderSpec("custom")
	if err != nil {
		t.Fatalf("ParseProviderSpec returned error: %v", err)
	}
	cfg, err := NewProviderConfig(ref, spec, "https://example.test/v1", "cred-1", "chat_completions")
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
	if _, err := cfg.WithTargetAlias(compatibility.PrimaryTargetSelector); !errors.Is(err, ErrInvalidProviderConfig) {
		t.Fatalf("WithTargetAlias(%s) error = %v, want ErrInvalidProviderConfig", compatibility.PrimaryTargetSelector, err)
	}
	if _, err := cfg.WithTargetAlias("gpt.5"); !errors.Is(err, ErrInvalidProviderConfig) {
		t.Fatalf("WithTargetAlias(gpt.5) error = %v, want ErrInvalidProviderConfig", err)
	}
}
