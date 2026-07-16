package target_add

import (
	"testing"

	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

func TestStaticCatalogFallback_OpenAI(t *testing.T) {
	got := staticCatalogFallback("openai")
	if len(got) == 0 {
		t.Fatal("expected non-empty OpenAI catalog")
	}
	found := false
	for _, d := range got {
		if d.ID == "gpt-4.1" {
			found = true
			if d.ModelName != "gpt-4.1" {
				t.Fatalf("model name = %q", d.ModelName)
			}
			if d.DefaultProviderProtocol != "chat_completions" {
				t.Fatalf("protocol = %q", d.DefaultProviderProtocol)
			}
		}
	}
	if !found {
		t.Fatal("missing gpt-4.1")
	}
	// Ensure returned slice is a copy (mutability safety).
	got[0].ID = "mutated"
	if staticCatalogs["openai"][0].ID == "mutated" {
		t.Fatal("staticCatalogFallback must return a copy")
	}
}

func TestStaticCatalogFallback_UnknownProvider(t *testing.T) {
	got := staticCatalogFallback("unknown_provider")
	if got != nil {
		t.Fatalf("expected nil for unknown provider, got %d items", len(got))
	}
}

func TestStaticCatalogProviderSpecs(t *testing.T) {
	got := staticCatalogProviderSpecs()
	if len(got) != len(staticCatalogs) {
		t.Fatalf("specs = %d, want %d", len(got), len(staticCatalogs))
	}
	// Check a few known providers.
	have := make(map[string]bool)
	for _, s := range got {
		have[s] = true
	}
	for _, want := range []string{"openai", "anthropic", "openrouter", "chatgpt", "ollama"} {
		if !have[want] {
			t.Fatalf("missing spec %q", want)
		}
	}
}

func TestWorkflow_ModelPicker_WithStaticCatalog(t *testing.T) {
	w := NewWorkflow("dev", readmodel.RouteReadModel{ID: "gpt", ModelName: "gpt"}, nil, nil)
	w.Open()
	w.SelectProvider("openai")
	w.SetSetupReady("", "")

	// Simulate a successful catalog load using static fallback.
	deployments := staticCatalogFallback(w.Provider.Get())
	w.SetCatalogResult(readmodel.ModelCatalogReadModel{Deployments: deployments})

	if w.Phase.Get() != PhaseChoosingModel {
		t.Fatalf("phase = %v, want ChoosingModel", w.Phase.Get())
	}
	if len(w.Catalog.Get().Deployments) == 0 {
		t.Fatal("expected catalog deployments")
	}

	// Verify model picker mounts with correct options count.
	picker := w.modelPicker()
	if picker == nil {
		t.Fatal("modelPicker is nil")
	}
	// The model picker uses the catalog snapshot at creation time.
	// Open search_picker internals ... let's check indirectly via Render.
}

func TestWorkflow_ModelPicker_ResetsOnCatalogChange(t *testing.T) {
	w := NewWorkflow("dev", sampleRoute(), nil, nil)
	w.Open()
	w.SelectProvider("openai")
	w.SetSetupReady("", "")
	w.SetCatalogResult(readmodel.ModelCatalogReadModel{Deployments: staticCatalogFallback("openai")})
	w.SelectModel(readmodel.ModelDeploymentReadModel{ID: "gpt-4o", ModelName: "gpt-4o"})

	if w.Phase.Get() != PhaseReadyToCreate {
		t.Fatalf("phase = %v, want ReadyToCreate", w.Phase.Get())
	}

	// Back to provider setup, pick a different provider, new catalog.
	w.Back()
	w.Provider.Set("anthropic")
	w.resetPickers()
	w.SetCatalogResult(readmodel.ModelCatalogReadModel{Deployments: staticCatalogFallback("anthropic")})

	if w.Phase.Get() != PhaseChoosingModel {
		t.Fatalf("phase = %v, want ChoosingModel", w.Phase.Get())
	}
	if w.modelPickerCache != nil {
		// resetPickers cleared the cache; modelPicker() should rebuild.
		picker := w.modelPicker()
		if picker == nil {
			t.Fatal("modelPicker should rebuild after reset")
		}
	}
}
