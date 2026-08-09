package target_config

import (
	"strings"
	"testing"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
	"github.com/swobuforge/swobu/internal/profile"
)

func TestModelSelectionDefaultsProtocolAndAdvancesSelectionToCreate(t *testing.T) {
	config := NewTargetConfig("dev", readmodel.RouteReadModel{ID: "chat"}, nil, nil)
	config.Draft.Set(readmodel.TargetDraft{
		ProviderSpec:  "openai",
		CredentialRef: "env:OPENAI_API_KEY",
	})
	config.Catalog.Set(catalogOperationState{Result: readmodel.ModelCatalogReadModel{Deployments: []readmodel.ModelDeploymentReadModel{{
		ID:        "gpt-5.6",
		Name:      "gpt-5.6",
		ModelName: "gpt-5.6",
	}}}})
	config.Open()

	h, err := testkit.NewHarness(config)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()
	if frame := h.Frame(); !strings.Contains(frame, "> model") {
		t.Fatalf("model row must own selection before opening picker:\n%s", frame)
	}

	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})

	frame := h.Frame()
	if !strings.Contains(frame, "protocol          OpenAI · Responses") {
		t.Fatalf("protocol row must show the provider default after model choice:\n%s", frame)
	}
	if !strings.Contains(frame, "> create") {
		t.Fatalf("create row must own selection after the model establishes a valid target:\n%s", frame)
	}
	if got := strings.Count(frame, ">"); got != 1 {
		t.Fatalf("selection markers = %d, want exactly one on create:\n%s", got, frame)
	}
}

func TestModelSelectionDefaultsToFirstResolvedProtocolAndRemainsEditable(t *testing.T) {
	providers := []profile.ProviderID{
		profile.ProviderSpecVLLM,
		profile.ProviderSpecLMStudio,
		profile.ProviderSpecOllama,
	}
	for _, provider := range providers {
		t.Run(string(provider), func(t *testing.T) {
			config := authoringConfig(t, provider, "", "")
			model := readmodel.ModelDeploymentReadModel{ID: "served-model", Name: "served-model", ModelName: "served-model"}
			config.Catalog.Set(catalogOperationState{Result: readmodel.ModelCatalogReadModel{Deployments: []readmodel.ModelDeploymentReadModel{model}}})

			config.SelectModel(model)

			if got := config.Draft.Get().ProviderProtocol; got != "responses" {
				t.Fatalf("initial protocol = %q, want first resolved option responses", got)
			}
			if !config.readyToCreate() {
				t.Fatal("target must be valid after model selection without another protocol decision")
			}

			config.selectProtocol("messages")
			if got := config.Draft.Get().ProviderProtocol; got != "messages" {
				t.Fatalf("edited protocol = %q, want messages", got)
			}
		})
	}
}

func TestCatalogHydrationDefaultsOnlyAnAbsentOrInvalidProtocol(t *testing.T) {
	model := readmodel.ModelDeploymentReadModel{ID: "served-model", Name: "served-model", ModelName: "served-model"}
	for _, test := range []struct {
		name string
		seed string
		want string
	}{
		{name: "absent", want: "responses"},
		{name: "invalid", seed: "unknown", want: "responses"},
		{name: "explicit", seed: "messages", want: "messages"},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := authoringConfig(t, profile.ProviderSpecVLLM, "", "")
			config.SelectedModel.Set(model)
			config.Draft.Update(func(d readmodel.TargetDraft) readmodel.TargetDraft {
				d.ProviderProtocol = test.seed
				return d
			})

			if !config.hydrateSelectedModel([]readmodel.ModelDeploymentReadModel{model}) {
				t.Fatal("selected model was not hydrated")
			}
			if got := config.Draft.Get().ProviderProtocol; got != test.want {
				t.Fatalf("hydrated protocol = %q, want %q", got, test.want)
			}
		})
	}
}

func TestIncompleteCreateStatusDoesNotParticipateInSelection(t *testing.T) {
	config := authoringConfig(t, profile.ProviderSpecAzure, "https://example.services.ai.azure.com/api/projects/demo", "env:AZURE_OPENAI_API_KEY")
	config.Route = readmodel.RouteReadModel{ID: "openai"}
	deployment := readmodel.ModelDeploymentReadModel{ID: "gpt-5.6-sol", Name: "gpt-5.6-sol", ModelName: "gpt-5.6-sol"}
	config.Catalog.Set(catalogOperationState{Result: readmodel.ModelCatalogReadModel{Deployments: []readmodel.ModelDeploymentReadModel{deployment}}})
	config.SelectModel(deployment)
	config.Draft.Update(func(d readmodel.TargetDraft) readmodel.TargetDraft {
		d.ProviderProtocol = ""
		return d
	})

	h, err := testkit.NewHarness(config)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()
	frame := h.FrameTrimmed()
	if !strings.Contains(frame, "> protocol") {
		t.Fatalf("required protocol did not own selection:\n%s", frame)
	}
	if got := strings.Count(frame, "complete setup"); got != 1 {
		t.Fatalf("complete setup occurrences = %d, want one status line:\n%s", got, frame)
	}
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	frame = h.FrameTrimmed()
	if !strings.Contains(frame, "search") || !strings.Contains(frame, "Responses") {
		t.Fatalf("Enter on required protocol did not open its chooser:\n%s", frame)
	}
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEscape})

	h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
	frame = h.FrameTrimmed()
	if strings.Contains(frame, "> create") {
		t.Fatalf("incomplete create participated in selection:\n%s", frame)
	}
}
