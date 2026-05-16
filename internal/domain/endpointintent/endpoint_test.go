package endpointintent

import (
	"errors"
	"testing"
)

func TestEndpoint_RequiresAtLeastOneTarget(t *testing.T) {
	name, err := ParseEndpointName("alpha")
	if err != nil {
		t.Fatalf("ParseEndpointName returned error: %v", err)
	}
	selected, err := ParseProviderConfigRef("cfg-a")
	if err != nil {
		t.Fatalf("ParseProviderConfigRef returned error: %v", err)
	}

	_, err = NewEndpoint(name, nil, selected)
	if !errors.Is(err, ErrInvalidEndpoint) {
		t.Fatalf("expected ErrInvalidEndpoint, got %v", err)
	}
}

func TestEndpoint_ProviderConfigOrderPreservedFromSlicePosition(t *testing.T) {
	name, err := ParseEndpointName("alpha")
	if err != nil {
		t.Fatalf("ParseEndpointName returned error: %v", err)
	}
	selected, err := ParseProviderConfigRef("cfg-a")
	if err != nil {
		t.Fatalf("ParseProviderConfigRef returned error: %v", err)
	}
	spec, err := ParseProviderSpec("openai_compatible")
	if err != nil {
		t.Fatalf("ParseProviderSpec returned error: %v", err)
	}
	secondRef, err := ParseProviderConfigRef("cfg-b")
	if err != nil {
		t.Fatalf("ParseProviderConfigRef returned error: %v", err)
	}
	firstRef, err := ParseProviderConfigRef("cfg-a")
	if err != nil {
		t.Fatalf("ParseProviderConfigRef returned error: %v", err)
	}
	second, err := NewProviderConfig(
		secondRef,
		spec,
		"https://b.test/v1",
		"cred-b",
	)
	if err != nil {
		t.Fatalf("NewProviderConfig(second) returned error: %v", err)
	}
	first, err := NewProviderConfig(
		firstRef,
		spec,
		"https://a.test/v1",
		"cred-a",
	)
	if err != nil {
		t.Fatalf("NewProviderConfig(first) returned error: %v", err)
	}

	endpoint, err := NewEndpoint(name, []ProviderConfig{second, first}, selected)
	if err != nil {
		t.Fatalf("NewEndpoint returned error: %v", err)
	}

	providerConfigs := endpoint.ProviderConfigs()
	if len(providerConfigs) != 2 {
		t.Fatalf("len(providerConfigs) = %d, want 2", len(providerConfigs))
	}
	if got := providerConfigs[0].Ref().String(); got != "cfg-b" {
		t.Fatalf("providerConfigs[0] ref = %q, want %q", got, "cfg-b")
	}
	if got := providerConfigs[1].Ref().String(); got != "cfg-a" {
		t.Fatalf("providerConfigs[1] ref = %q, want %q", got, "cfg-a")
	}
	if got := endpoint.SelectedProviderConfig().Ref().String(); got != "cfg-a" {
		t.Fatalf("selected provider config ref = %q, want %q", got, "cfg-a")
	}
}

func TestEndpoint_RejectsDuplicateProviderConfigRef(t *testing.T) {
	name, err := ParseEndpointName("alpha")
	if err != nil {
		t.Fatalf("ParseEndpointName returned error: %v", err)
	}
	spec, err := ParseProviderSpec("openai_compatible")
	if err != nil {
		t.Fatalf("ParseProviderSpec returned error: %v", err)
	}
	ref, err := ParseProviderConfigRef("cfg-a")
	if err != nil {
		t.Fatalf("ParseProviderConfigRef returned error: %v", err)
	}
	first, err := NewProviderConfig(
		ref,
		spec,
		"https://a.test/v1",
		"cred-a",
	)
	if err != nil {
		t.Fatalf("NewProviderConfig(first) returned error: %v", err)
	}
	second, err := NewProviderConfig(
		ref,
		spec,
		"https://b.test/v1",
		"cred-b",
	)
	if err != nil {
		t.Fatalf("NewProviderConfig(second) returned error: %v", err)
	}

	_, err = NewEndpoint(name, []ProviderConfig{first, second}, ref)
	if !errors.Is(err, ErrInvalidEndpoint) {
		t.Fatalf("expected ErrInvalidEndpoint, got %v", err)
	}
}

func TestEndpoint_RequiresSelectedProviderConfigToExist(t *testing.T) {
	name, err := ParseEndpointName("alpha")
	if err != nil {
		t.Fatalf("ParseEndpointName returned error: %v", err)
	}
	spec, err := ParseProviderSpec("openai_compatible")
	if err != nil {
		t.Fatalf("ParseProviderSpec returned error: %v", err)
	}
	firstRef, err := ParseProviderConfigRef("cfg-a")
	if err != nil {
		t.Fatalf("ParseProviderConfigRef returned error: %v", err)
	}
	secondRef, err := ParseProviderConfigRef("cfg-b")
	if err != nil {
		t.Fatalf("ParseProviderConfigRef returned error: %v", err)
	}
	missingRef, err := ParseProviderConfigRef("cfg-missing")
	if err != nil {
		t.Fatalf("ParseProviderConfigRef returned error: %v", err)
	}
	first, err := NewProviderConfig(firstRef, spec, "https://a.test/v1", "cred-a")
	if err != nil {
		t.Fatalf("NewProviderConfig(first) returned error: %v", err)
	}
	second, err := NewProviderConfig(secondRef, spec, "https://b.test/v1", "cred-b")
	if err != nil {
		t.Fatalf("NewProviderConfig(second) returned error: %v", err)
	}

	_, err = NewEndpoint(name, []ProviderConfig{first, second}, missingRef)
	if !errors.Is(err, ErrInvalidEndpoint) {
		t.Fatalf("expected ErrInvalidEndpoint, got %v", err)
	}
}

func TestEndpoint_RejectsDuplicateTargetAlias(t *testing.T) {
	name, err := ParseEndpointName("alpha")
	if err != nil {
		t.Fatalf("ParseEndpointName returned error: %v", err)
	}
	spec, err := ParseProviderSpec("openai_compatible")
	if err != nil {
		t.Fatalf("ParseProviderSpec returned error: %v", err)
	}
	firstRef, err := ParseProviderConfigRef("cfg-a")
	if err != nil {
		t.Fatalf("ParseProviderConfigRef returned error: %v", err)
	}
	secondRef, err := ParseProviderConfigRef("cfg-b")
	if err != nil {
		t.Fatalf("ParseProviderConfigRef returned error: %v", err)
	}
	first, err := NewProviderConfig(firstRef, spec, "https://a.test/v1", "cred-a")
	if err != nil {
		t.Fatalf("NewProviderConfig(first) returned error: %v", err)
	}
	second, err := NewProviderConfig(secondRef, spec, "https://b.test/v1", "cred-b")
	if err != nil {
		t.Fatalf("NewProviderConfig(second) returned error: %v", err)
	}
	first, _ = first.WithTargetAlias("fast")
	second, _ = second.WithTargetAlias("fast")

	_, err = NewEndpoint(name, []ProviderConfig{first, second}, firstRef)
	if !errors.Is(err, ErrInvalidEndpoint) {
		t.Fatalf("expected ErrInvalidEndpoint, got %v", err)
	}
}

func TestEndpoint_RejectsTargetAliasThatCollidesWithModelSelector(t *testing.T) {
	name, err := ParseEndpointName("alpha")
	if err != nil {
		t.Fatalf("ParseEndpointName returned error: %v", err)
	}
	spec, err := ParseProviderSpec("openai_compatible")
	if err != nil {
		t.Fatalf("ParseProviderSpec returned error: %v", err)
	}
	firstRef, err := ParseProviderConfigRef("cfg-a")
	if err != nil {
		t.Fatalf("ParseProviderConfigRef returned error: %v", err)
	}
	secondRef, err := ParseProviderConfigRef("cfg-b")
	if err != nil {
		t.Fatalf("ParseProviderConfigRef returned error: %v", err)
	}
	first, err := NewProviderConfig(firstRef, spec, "https://a.test/v1", "cred-a")
	if err != nil {
		t.Fatalf("NewProviderConfig(first) returned error: %v", err)
	}
	second, err := NewProviderConfig(secondRef, spec, "https://b.test/v1", "cred-b")
	if err != nil {
		t.Fatalf("NewProviderConfig(second) returned error: %v", err)
	}
	first, _ = first.WithModelID("fast")
	second, _ = second.WithModelID("deep")
	second, _ = second.WithTargetAlias("fast")

	_, err = NewEndpoint(name, []ProviderConfig{first, second}, firstRef)
	if !errors.Is(err, ErrInvalidEndpoint) {
		t.Fatalf("expected ErrInvalidEndpoint, got %v", err)
	}
}
