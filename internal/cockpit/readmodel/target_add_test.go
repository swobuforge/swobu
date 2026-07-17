package readmodel

import "testing"

func TestProviderOptionReadModel_FieldAccess(t *testing.T) {
	o := ProviderOptionReadModel{
		ProviderSpec: "openai",
		DisplayName:  "OpenAI",
		SetupHint:    "openai",
	}
	if o.ProviderSpec != "openai" {
		t.Fatalf("ProviderSpec = %q", o.ProviderSpec)
	}
	if o.DisplayName != "OpenAI" {
		t.Fatalf("DisplayName = %q", o.DisplayName)
	}
}

func TestModelCatalogReadModel_ErrorOrDeployments(t *testing.T) {
	// Success case.
	cat := ModelCatalogReadModel{
		Deployments: []ModelDeploymentReadModel{
			{ID: "gpt-4.1", Name: "GPT 4.1", ModelName: "gpt-4.1", DefaultProviderProtocol: "chat_completions"},
		},
		ResolvedProviderProtocol: "chat_completions",
	}
	if len(cat.Deployments) != 1 {
		t.Fatalf("deployments = %d, want 1", len(cat.Deployments))
	}
	if cat.Deployments[0].DefaultProviderProtocol != "chat_completions" {
		t.Fatalf("protocol = %q", cat.Deployments[0].DefaultProviderProtocol)
	}

	// Error case.
	errCat := ModelCatalogReadModel{Error: "401 unauthorized"}
	if errCat.Error != "401 unauthorized" {
		t.Fatalf("error = %q", errCat.Error)
	}
}

func TestAuthSessionReadModel_States(t *testing.T) {
	pending := AuthSessionReadModel{
		ProviderSpec: "chatgpt",
		SessionID:    "sess-1",
		State:        "pending",
		AuthorizeURL: "https://auth.openai.com/",
		UserCode:     "ABCD-1234",
	}
	if pending.State != "pending" {
		t.Fatalf("state = %q, want pending", pending.State)
	}
	if pending.UserCode != "ABCD-1234" {
		t.Fatalf("userCode = %q", pending.UserCode)
	}

	failed := AuthSessionReadModel{
		ProviderSpec: "chatgpt",
		SessionID:    "sess-1",
		State:        "error",
		ErrorMessage: "session expired",
	}
	if failed.ErrorMessage != "session expired" {
		t.Fatalf("errorMessage = %q", failed.ErrorMessage)
	}
}

func TestPlacementOptionReadModel_DefaultFallback(t *testing.T) {
	p := PlacementOptionReadModel{
		Label:  "fallback after last step",
		Rank:   3,
		Weight: 1,
		Kind:   PlacementFallback,
	}
	if p.Kind != PlacementFallback {
		t.Fatalf("kind = %v, want PlacementFallback", p.Kind)
	}
	if p.Rank != 3 {
		t.Fatalf("rank = %d, want 3", p.Rank)
	}
	if got := p.Summary(); got != "fallback after last step" {
		t.Fatalf("summary = %q, want fallback after last step", got)
	}
}
