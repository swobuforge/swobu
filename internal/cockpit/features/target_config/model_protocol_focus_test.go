package target_config

import (
	"strings"
	"testing"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
	"github.com/swobuforge/swobu/internal/profile"
)

func TestModelSelectionAdvancesSelectionToRequiredProtocol(t *testing.T) {
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
	if !strings.Contains(frame, "> protocol") {
		t.Fatalf("protocol row must own selection after model choice:\n%s", frame)
	}
	if got := strings.Count(frame, ">"); got != 1 {
		t.Fatalf("selection markers = %d, want exactly one on protocol:\n%s", got, frame)
	}
}

func TestIncompleteCreateStatusDoesNotParticipateInSelection(t *testing.T) {
	config := authoringConfig(t, profile.ProviderSpecAzure, "https://example.services.ai.azure.com/api/projects/demo", "env:AZURE_OPENAI_API_KEY")
	config.Route = readmodel.RouteReadModel{ID: "openai"}
	deployment := readmodel.ModelDeploymentReadModel{ID: "gpt-5.6-sol", Name: "gpt-5.6-sol", ModelName: "gpt-5.6-sol"}
	config.Catalog.Set(catalogOperationState{Result: readmodel.ModelCatalogReadModel{Deployments: []readmodel.ModelDeploymentReadModel{deployment}}})
	config.SelectModel(deployment)

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
