package selectors

import (
	"testing"

	"github.com/metrofun/swobu/internal/terminalui/apps/cockpit/app/state"
)

func TestProviderConfigRequestModelID_PrefersAlias(t *testing.T) {
	snapshot := &state.EndpointSnapshot{
		Name:                      "alpha",
		SelectedProviderConfigRef: "backend-a",
		ProviderConfigs: []state.ProviderConfigSnapshot{
			{Ref: "backend-a", ProviderSpec: "openai", ModelID: "gpt-5.3", TargetAlias: "fast"},
		},
	}
	if got := ProviderConfigRequestModelID(snapshot, "backend-a"); got != "fast" {
		t.Fatalf("selector = %q, want %q", got, "fast")
	}
}

func TestProviderConfigRequestModelID_UsesMechanicalFallback(t *testing.T) {
	snapshot := &state.EndpointSnapshot{
		Name:                      "alpha",
		SelectedProviderConfigRef: "backend-a",
		ProviderConfigs: []state.ProviderConfigSnapshot{
			{Ref: "backend-a", ProviderSpec: "openai", ModelID: "gpt-5.3"},
		},
	}
	if got := ProviderConfigRequestModelID(snapshot, "backend-a"); got != "gpt-5.3" {
		t.Fatalf("selector = %q, want %q", got, "gpt-5.3")
	}
}

func TestProviderConfigRequestModelID_DisambiguatesMechanicalDuplicates(t *testing.T) {
	snapshot := &state.EndpointSnapshot{
		Name:                      "alpha",
		SelectedProviderConfigRef: "backend-a",
		ProviderConfigs: []state.ProviderConfigSnapshot{
			{Ref: "backend-a", ProviderSpec: "openai", ModelID: "gpt-5.3"},
			{Ref: "backend-b", ProviderSpec: "openai", ModelID: "gpt-5.3"},
		},
	}
	if got := ProviderConfigRequestModelID(snapshot, "backend-a"); got != "openai:gpt-5.3:backend-a" {
		t.Fatalf("selector = %q, want %q", got, "openai:gpt-5.3:backend-a")
	}
	if got := ProviderConfigRequestModelID(snapshot, "backend-b"); got != "openai:gpt-5.3:backend-b" {
		t.Fatalf("selector = %q, want %q", got, "openai:gpt-5.3:backend-b")
	}
}
