package routing

import (
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
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
