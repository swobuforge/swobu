package requestpath

import (
	"testing"

	"github.com/swobuforge/swobu/internal/domain/protocolsurface"
)

func TestBackendModelCapabilityCatalog_UsesExactBeforeWildcard(t *testing.T) {
	catalog := newBackendModelCapabilityCatalog([]backendModelCapabilityRecord{
		{
			ProviderSpec:   "custom",
			ProtocolKind:   protocolsurface.Responses,
			BackendModelID: "*",
			Capability: CapabilitySnapshot{
				ToolChoice: ToolChoiceCapability{ImmediateDowngradeRetry: true},
			},
		},
		{
			ProviderSpec:   "custom",
			ProtocolKind:   protocolsurface.Responses,
			BackendModelID: "gpt-special",
			Capability: CapabilitySnapshot{
				ToolChoice: ToolChoiceCapability{ImmediateDowngradeRetry: false},
			},
		},
	})

	got := catalog.SnapshotFor(BackendModelEntity{
		ProviderSpec:   "custom",
		ProtocolKind:   protocolsurface.Responses,
		BackendModelID: "gpt-special",
	})
	if got.ToolChoice.ImmediateDowngradeRetry {
		t.Fatalf("immediate_downgrade_retry = true, want false from exact model capability: %#v", got)
	}
}

func TestBackendModelCapabilityCatalog_DefaultsConservativelyWhenUnknown(t *testing.T) {
	catalog := defaultBackendModelCapabilityCatalog()

	tests := []BackendModelEntity{
		{ProviderSpec: "custom", ProtocolKind: protocolsurface.ChatCompletions, BackendModelID: "gpt-4.1"},
		{ProviderSpec: "unknown-provider", ProtocolKind: protocolsurface.Responses, BackendModelID: "gpt-4.1"},
		{ProviderSpec: "custom", ProtocolKind: protocolsurface.Responses},
	}
	for _, tc := range tests {
		got := catalog.SnapshotFor(tc)
		if got.ToolChoice.ImmediateDowngradeRetry {
			t.Fatalf("entity=%#v produced capability=%#v, want conservative zero capability", tc, got)
		}
	}
}

func TestDefaultBackendModelCapabilityCatalog_EnablesResponsesForKnownProvider(t *testing.T) {
	catalog := defaultBackendModelCapabilityCatalog()

	got := catalog.SnapshotFor(BackendModelEntity{
		ProviderSpec:   "custom",
		ProtocolKind:   protocolsurface.Responses,
		BackendModelID: "gpt-4.1",
	})
	if !got.ToolChoice.ImmediateDowngradeRetry {
		t.Fatalf("immediate_downgrade_retry = false, want true for known responses backend model entity: %#v", got)
	}
}

func TestDefaultBackendModelCapabilityCatalog_UsesModelSpecificProviderChatFacts(t *testing.T) {
	catalog := defaultBackendModelCapabilityCatalog()

	got := catalog.SnapshotFor(BackendModelEntity{
		ProviderSpec:   "openrouter",
		ProtocolKind:   protocolsurface.ChatCompletions,
		BackendModelID: "nvidia/nemotron-3-super-120b-a12b",
	})
	if !got.ToolChoice.ImmediateDowngradeRetry {
		t.Fatalf("immediate_downgrade_retry = false, want true for openrouter model override: %#v", got)
	}
}

func TestDefaultBackendModelCapabilityCatalog_DisablesUnknownProviderChatModel(t *testing.T) {
	catalog := defaultBackendModelCapabilityCatalog()

	got := catalog.SnapshotFor(BackendModelEntity{
		ProviderSpec:   "openrouter",
		ProtocolKind:   protocolsurface.ChatCompletions,
		BackendModelID: "unknown/model",
	})
	if got.ToolChoice.ImmediateDowngradeRetry {
		t.Fatalf("immediate_downgrade_retry = true, want false for unknown openrouter chat model: %#v", got)
	}
}
