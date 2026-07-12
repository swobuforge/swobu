package routing

import (
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/swobuforge/swobu/internal/terminalui/testharness"
)

func TestFirstRunCredentialSummaryMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		provider      string
		baseURL       string
		credentialRef string
		want          string
	}{
		{name: "openrouter requires chooser", provider: "openrouter", baseURL: "https://openrouter.ai/api/v1", want: "required"},
		{name: "ollama local defaults auto", provider: "ollama", baseURL: "http://127.0.0.1:11434/v1", want: "auto"},
		{name: "openai-compatible remote requires chooser", provider: "openai_compatible", baseURL: "https://api.example.com/v1", want: "required"},
		{name: "azure remote requires chooser", provider: "azure", baseURL: "https://contact-5464-resource.openai.azure.com/openai/v1", want: "required"},
		{name: "openai-compatible local still requires explicit credential", provider: "openai_compatible", baseURL: "http://localhost:11434/v1", want: "required"},
		{name: "existing credential without env is surfaced as required", provider: "ollama", baseURL: "http://127.0.0.1:11434/v1", credentialRef: "env:OLLAMA_API_KEY", want: "required"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := firstRunCredentialSummary(tt.provider, tt.baseURL, tt.credentialRef)
			if got != tt.want {
				t.Fatalf("firstRunCredentialSummary(provider=%q baseURL=%q credentialRef=%q)=%q want %q", tt.provider, tt.baseURL, tt.credentialRef, got, tt.want)
			}
		})
	}
}

func TestAppendCreateCredentialRows_KeychainShowsRawPasteEditor(t *testing.T) {
	t.Parallel()

	rows := appendCreateCredentialRows(nil, "openai", "keychain")
	if len(rows) == 0 {
		t.Fatal("expected keychain create extras to render")
	}
}

func TestCreateDraftProtocolModeRow_DefaultsAndResolvesProviderProtocol(t *testing.T) {
	t.Parallel()

	t.Run("openai-default-resolves-auto", func(t *testing.T) {
		t.Parallel()
		model := state.Model{CreateDraftProviderConfig: state.ProviderConfigSnapshot{ProviderSpec: "openai", ProviderProtocol: ""}}
		out := testharness.RenderSpec(model, createDraftProtocolModeRow(model), geom.Rect{W: 100, H: 2}).String()
		if !strings.Contains(out, "protocol") || !strings.Contains(out, "auto") {
			t.Fatalf("expected protocol row with default auto; render=%q", out)
		}
	})

	t.Run("anthropic-default-resolves-auto", func(t *testing.T) {
		t.Parallel()
		model := state.Model{CreateDraftProviderConfig: state.ProviderConfigSnapshot{ProviderSpec: "anthropic", ProviderProtocol: ""}}
		out := testharness.RenderSpec(model, createDraftProtocolModeRow(model), geom.Rect{W: 100, H: 2}).String()
		if !strings.Contains(out, "protocol") || !strings.Contains(out, "auto") {
			t.Fatalf("expected protocol row with default auto; render=%q", out)
		}
	})
}

func TestCreateDraftProtocolModeRow_DefaultsToAuto_Regression(t *testing.T) {
	t.Parallel()

	model := state.Model{
		CreateDraftProviderConfig: state.ProviderConfigSnapshot{
			ProviderSpec:     "openai",
			ProviderProtocol: "",
		},
	}
	out := testharness.RenderSpec(model, createDraftProtocolModeRow(model), geom.Rect{W: 100, H: 2}).String()
	if !strings.Contains(out, "protocol") || !strings.Contains(out, "auto") {
		t.Fatalf("expected protocol row with auto default; render=%q", out)
	}
}

func TestCreateDraftTestOrCreateRow_ReadyWithoutProbeGate(t *testing.T) {
	t.Parallel()

	model := state.Model{
		CreateDraftName: "acme",
		CreateDraftProviderConfig: state.ProviderConfigSnapshot{
			ProviderSpec:     "openai",
			BaseURL:          "https://api.openai.com/v1",
			CredentialRef:    "env:OPENAI_API_KEY",
			ProviderProtocol: "responses_stream",
			ModelID:          "gpt-5.4-mini",
		},
	}

	ready := testharness.RenderSpec(model, createDraftTestOrCreateRow(model), geom.Rect{W: 100, H: 2}).String()
	if !strings.Contains(ready, "ready") || !strings.Contains(ready, "create") {
		t.Fatalf("expected ready create row state; render=%q", ready)
	}
}
