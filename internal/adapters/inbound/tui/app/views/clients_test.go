package views

import (
	"testing"

	"github.com/metrofun/swobu/internal/adapters/inbound/tui/app/state"
	"github.com/metrofun/swobu/internal/domain/compatibility"
)

func TestSelectedClientRunModelID_AlwaysUsesPrimarySelector(t *testing.T) {
	model := state.Model{
		CurrentEndpoint: "alpha",
		EndpointSnapshots: []state.EndpointSnapshot{
			{
				Name:                      "alpha",
				SelectedProviderConfigRef: "backend-a",
				ProviderConfigs: []state.ProviderConfigSnapshot{
					{
						Ref:          "backend-a",
						ProviderSpec: "openrouter",
						ModelID:      "llama3.2:1b",
						TargetAlias:  "fast",
					},
				},
			},
		},
	}
	if got := selectedClientRunModelID(model); got != compatibility.PrimaryTargetSelector {
		t.Fatalf("run model id = %q, want %q", got, compatibility.PrimaryTargetSelector)
	}
}

func TestSelectedClientRunModelID_EmptyWithoutWorkspaceSnapshot(t *testing.T) {
	if got := selectedClientRunModelID(state.Model{}); got != "" {
		t.Fatalf("run model id = %q, want empty", got)
	}
}
