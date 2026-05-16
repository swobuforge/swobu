package views

import (
	"testing"

	"github.com/swobuforge/swobu/internal/app/requestpath"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
)

func TestSelectedClientRunModelID_AlwaysUsesPublicSwobuModel(t *testing.T) {
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
	if got := selectedClientRunModelID(model); got != requestpath.PublicModelIDSwobu {
		t.Fatalf("run model id = %q, want %q", got, requestpath.PublicModelIDSwobu)
	}
}

func TestSelectedClientRunModelID_EmptyWithoutWorkspaceSnapshot(t *testing.T) {
	if got := selectedClientRunModelID(state.Model{}); got != "" {
		t.Fatalf("run model id = %q, want empty", got)
	}
}
