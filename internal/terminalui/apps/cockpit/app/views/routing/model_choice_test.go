package routing

import (
	"testing"

	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/swobuforge/swobu/internal/terminalui/testharness"
)

func TestProviderModelCatalogChoicesAvailable_WorkspaceCatalogProvidersUsePicker(t *testing.T) {
	t.Parallel()

	spec := providerModelChoiceRowSpec{
		CreateMode: false,
		ProviderConfig: &state.ProviderConfigSnapshot{
			ProviderSpec: "openrouter",
		},
	}
	if !providerModelCatalogChoicesAvailable(spec) {
		t.Fatalf("workspace catalog-capable provider must use model picker UX")
	}
}

func TestProviderModelCatalogChoicesAvailable_CreateModeUsesDraftFlow(t *testing.T) {
	t.Parallel()

	spec := providerModelChoiceRowSpec{
		CreateMode: true,
		ProviderConfig: &state.ProviderConfigSnapshot{
			ProviderSpec: "openrouter",
		},
	}
	if providerModelCatalogChoicesAvailable(spec) {
		t.Fatalf("create mode should not use workspace picker path")
	}
}

func TestProviderModelCatalogChoicesAvailable_OpenAICompatibleUsesPicker(t *testing.T) {
	t.Parallel()

	spec := providerModelChoiceRowSpec{
		CreateMode: false,
		ProviderConfig: &state.ProviderConfigSnapshot{
			ProviderSpec: "openai_compatible",
		},
	}
	if !providerModelCatalogChoicesAvailable(spec) {
		t.Fatalf("OpenAI-compatible provider should use model picker UX")
	}
}

func TestProviderModelCatalogChoicesAvailable_AzureUsesPicker(t *testing.T) {
	t.Parallel()

	spec := providerModelChoiceRowSpec{
		CreateMode: false,
		ProviderConfig: &state.ProviderConfigSnapshot{
			ProviderSpec: "azure",
		},
	}
	if !providerModelCatalogChoicesAvailable(spec) {
		t.Fatalf("Azure provider should use model picker UX")
	}
}

func TestProviderProtocolChoiceRow_AzureUnsetFailsClosed_Visual(t *testing.T) {
	t.Parallel()

	model := state.Model{}
	spec := providerProtocolChoiceRowSpec{
		ProviderConfig: &state.ProviderConfigSnapshot{
			ProviderSpec: "azure",
		},
	}
	out := testharness.RenderSpec(model, buildProviderProtocolChoiceRow(model, spec), geom.Rect{W: 100, H: 2}).String()
	if err := testharness.AssertVisual("azure_protocol_unset").Viewport(100, 2).Now(out); err != nil {
		t.Fatalf("expected Azure protocol row to fail closed when unset: %v\nrender=%q", err, out)
	}
}

func TestApplyProviderModelSelection_ClearsAutoWhenDeploymentHasNoDefault(t *testing.T) {
	t.Parallel()

	actions := applyProviderModelSelection(
		profile.ProviderDeploymentRecord{
			Name:                       "gpt-4.1-mini",
			SupportedProviderProtocols: []string{"responses"},
		},
		&state.ProviderConfigSnapshot{
			ProviderSpec:     "openai",
			ProviderProtocol: "auto",
		},
		"alpha",
		true,
	)
	if len(actions) != 2 {
		t.Fatalf("actions len=%d want 2", len(actions))
	}
	if _, ok := actions[0].(state.SetCreateDraftModelIDAction); !ok {
		t.Fatalf("actions[0]=%T want state.SetCreateDraftModelIDAction", actions[0])
	}
	clearAction, ok := actions[1].(state.SetCreateDraftProviderProtocol)
	if !ok {
		t.Fatalf("actions[1]=%T want state.SetCreateDraftProviderProtocol", actions[1])
	}
	if clearAction.ProviderProtocol != "" {
		t.Fatalf("provider protocol=%q want cleared", clearAction.ProviderProtocol)
	}
}

func TestApplyProviderModelSelection_UsesExplicitDeploymentDefault(t *testing.T) {
	t.Parallel()

	actions := applyProviderModelSelection(
		profile.ProviderDeploymentRecord{
			Name:                       "gpt-4.1-mini",
			SupportedProviderProtocols: []string{"responses"},
			DefaultProviderProtocol:    "responses",
		},
		&state.ProviderConfigSnapshot{
			ProviderSpec:     "openai",
			ProviderProtocol: "auto",
		},
		"alpha",
		true,
	)
	if len(actions) != 2 {
		t.Fatalf("actions len=%d want 2", len(actions))
	}
	protocolAction, ok := actions[1].(state.SetCreateDraftProviderProtocol)
	if !ok {
		t.Fatalf("actions[1]=%T want state.SetCreateDraftProviderProtocol", actions[1])
	}
	if protocolAction.ProviderProtocol != "responses" {
		t.Fatalf("provider protocol=%q want responses", protocolAction.ProviderProtocol)
	}
}
