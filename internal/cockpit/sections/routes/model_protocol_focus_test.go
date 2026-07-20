package routes

import (
	"strings"
	"testing"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
)

func TestAddTargetModelSelectionStaysLocalOnRequiredProtocol(t *testing.T) {
	route := readmodel.RouteReadModel{ID: "chat", ModelName: "chat", Enabled: true}
	section := Section(readmodel.WorkspaceReadModel{
		ID: "dev", Slug: "dev", State: readmodel.WorkspaceExisting,
		Routes: []readmodel.RouteReadModel{route},
	}, nil)
	section.State.ExpandedRoute.Set(route.ID)
	section.AddTarget(route)
	config := section.TargetConfigs.CachedAdd(route.ID)
	config.Draft.Set(readmodel.TargetDraft{ProviderSpec: "openai", CredentialRef: "env:OPENAI_API_KEY"})
	config.SetCatalogResult(readmodel.ModelCatalogReadModel{Deployments: []readmodel.ModelDeploymentReadModel{{
		ID: "gpt-5.6", Name: "gpt-5.6", ModelName: "gpt-5.6",
	}}}, nil)

	h, err := testkit.NewHarness(section)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()
	if frame := h.Frame(); !strings.Contains(frame, "> model") {
		t.Fatalf("model row must own selection before picker opens:\n%s", frame)
	}

	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})

	frame := h.Frame()
	if !strings.Contains(frame, "> protocol") {
		t.Fatalf("selection escaped target form after model choice:\n%s", frame)
	}
	if got := strings.Count(frame, ">"); got != 1 {
		t.Fatalf("selection markers = %d, want exactly one on protocol:\n%s", got, frame)
	}

	h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
	frame = h.Frame()
	if !strings.Contains(frame, "> add model route") || strings.Contains(frame, "> protocol") || strings.Contains(frame, "> create") {
		t.Fatalf("protocol handoff must skip incomplete create and continue to the next action:\n%s", frame)
	}
}
