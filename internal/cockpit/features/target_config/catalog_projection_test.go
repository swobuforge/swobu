package target_config

import (
	"testing"

	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/domain/endpointintent"
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

func TestTargetConfig_ModelPicker_WithStaticCatalog(t *testing.T) {
	w := NewTargetConfig("dev", readmodel.RouteReadModel{ID: "gpt", ModelName: "gpt"}, nil, nil)
	w.Open()
	w.SelectProvider("openai")
	w.SetSetupReady("env:OPENAI_API_KEY", "")

	// Simulate a successful catalog load using static fallback.
	deployments := staticCatalogFallback(w.Draft.Get().ProviderSpec)
	w.SetCatalogResult(readmodel.ModelCatalogReadModel{Deployments: deployments})

	if w.Phase.Get() != PhaseConfiguring {
		t.Fatalf("phase = %v, want ChoosingModel", w.Phase.Get())
	}
	if len(w.Catalog.Get().Deployments) == 0 {
		t.Fatal("expected catalog deployments")
	}

	// Verify model picker builds with the catalog deployments.
	picker := ModelPicker(w, nil)
	if picker == nil {
		t.Fatal("model picker is nil")
	}
}

func TestTargetConfig_ModelPicker_ResetsOnCatalogChange(t *testing.T) {
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)
	w.Open()
	w.SelectProvider("openai")
	w.SetSetupReady("env:OPENAI_API_KEY", "")
	w.SetCatalogResult(readmodel.ModelCatalogReadModel{Deployments: staticCatalogFallback("openai")})
	w.SelectModel(readmodel.ModelDeploymentReadModel{ID: "gpt-4o", ModelName: "gpt-4o"})
	w.SelectProtocol("responses_stream")

	if w.Phase.Get() != PhaseReadyToCreate {
		t.Fatalf("phase = %v, want ReadyToCreate", w.Phase.Get())
	}

	// Back to provider setup, pick a different provider, new catalog.
	w.Back()
	w.Draft.Update(func(d endpointintent.TargetDraft) endpointintent.TargetDraft { d.ProviderSpec = "anthropic"; return d })
	w.SetCatalogResult(readmodel.ModelCatalogReadModel{Deployments: staticCatalogFallback("anthropic")})

	if w.Phase.Get() != PhaseConfiguring {
		t.Fatalf("phase = %v, want ChoosingModel", w.Phase.Get())
	}
	// The model picker is built fresh from the new catalog.
	if picker := ModelPicker(w, nil); picker == nil {
		t.Fatal("model picker should build from the new catalog")
	}
}
