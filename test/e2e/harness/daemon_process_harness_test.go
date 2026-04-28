package harness

import (
	"strings"
	"testing"

	"github.com/metrofun/swobu/internal/domain/endpointintent"
	"github.com/metrofun/swobu/internal/domain/protocolsurface"
)

func TestRenderRuntimeConfigYAML_IncludesModelIDAndTargetAlias(t *testing.T) {
	name, err := endpointintent.ParseEndpointName("acme")
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
	provider, err := endpointintent.NewProviderConfig(ref, spec, "https://example.test/v1", "env", protocolsurface.ChatCompletions)
	if err != nil {
		t.Fatalf("NewProviderConfig returned error: %v", err)
	}
	provider, err = provider.WithModelID("gpt-4.1-mini")
	if err != nil {
		t.Fatalf("WithModelID returned error: %v", err)
	}
	provider, err = provider.WithTargetAlias("fast")
	if err != nil {
		t.Fatalf("WithTargetAlias returned error: %v", err)
	}
	endpoint, err := endpointintent.NewEndpoint(name, []endpointintent.ProviderConfig{provider}, ref)
	if err != nil {
		t.Fatalf("NewEndpoint returned error: %v", err)
	}

	yaml := renderRuntimeConfigYAML([]endpointintent.Endpoint{endpoint}, "127.0.0.1:8080")
	for _, want := range []string{
		"model_id: gpt-4.1-mini",
		"target_alias: fast",
		"selected_provider_config_ref: backend-a",
	} {
		if !strings.Contains(yaml, want) {
			t.Fatalf("rendered yaml missing %q:\n%s", want, yaml)
		}
	}
}
