package readmodel

import "testing"

func TestWorkspaceReadModel_ResetDraftInputPreservesAmbientProviderOptions(t *testing.T) {
	options := []ProviderOptionReadModel{{ProviderSpec: "openai", DisplayName: "OpenAI"}}
	workspace := WorkspaceReadModel{
		ID: "+", Slug: "dev", State: WorkspaceDraft,
		WorkspaceURL:    "http://127.0.0.1:7926/c/dev",
		Routes:          []RouteReadModel{{ID: "gpt"}},
		ProviderOptions: options,
	}

	reset := workspace.ResetDraftInput()

	if reset.ID != "+" || !reset.IsDraft() || reset.Slug != "" || reset.WorkspaceURL != "" || len(reset.Routes) != 0 {
		t.Fatalf("reset draft input = %#v", reset)
	}
	if len(reset.ProviderOptions) != 1 || reset.ProviderOptions[0].ProviderSpec != "openai" {
		t.Fatalf("reset provider options = %#v, want OpenAI", reset.ProviderOptions)
	}
	options[0].ProviderSpec = "mutated"
	if reset.ProviderOptions[0].ProviderSpec != "openai" {
		t.Fatal("canonical draft retained caller provider-option backing storage")
	}
}

func TestWorkspaceReadModel_RoutingWorkspaceIDDoesNotRepairMissingIdentity(t *testing.T) {
	workspace := WorkspaceReadModel{Slug: "partial", State: WorkspaceExisting}

	if got := workspace.RoutingWorkspaceID(); got != "" {
		t.Fatalf("routing workspace ID = %q, want empty for malformed non-draft projection", got)
	}
}
