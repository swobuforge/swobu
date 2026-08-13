package routes

import (
	"strings"
	"testing"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
)

func TestAddTargetModelSelectionStaysLocalOnDefaultedProtocol(t *testing.T) {
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
	if !strings.Contains(frame, "protocol          OpenAI · Responses") || !strings.Contains(frame, "> create") {
		t.Fatalf("selection escaped target form after model choice:\n%s", frame)
	}
	if got := strings.Count(frame, ">"); got != 1 {
		t.Fatalf("selection markers = %d, want exactly one on create:\n%s", got, frame)
	}

	h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
	frame = h.Frame()
	if !strings.Contains(frame, "> add model route") || strings.Contains(frame, "> create") {
		t.Fatalf("create handoff must continue to the next section action:\n%s", frame)
	}
}

func TestAddTargetProviderSelectionKeepsInlineConfigMounted(t *testing.T) {
	route := readmodel.RouteReadModel{ID: "chat", ModelName: "chat", Enabled: true}
	section := Section(readmodel.WorkspaceReadModel{
		ID: "dev", Slug: "dev", State: readmodel.WorkspaceExisting,
		Routes:          []readmodel.RouteReadModel{route},
		ProviderOptions: []readmodel.ProviderOptionReadModel{{ProviderSpec: "kimi", DisplayName: "Kimi"}},
	}, nil)
	section.State.ExpandedRoute.Set(route.ID)
	section.AddTarget(route)

	h, err := testkit.NewHarness(section)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()

	for range 16 {
		if frame := h.Frame(); strings.Contains(frame, "> Kimi") {
			break
		}
		h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
	}
	if frame := h.Frame(); !strings.Contains(frame, "> Kimi") {
		t.Fatalf("provider picker did not select Kimi through mounted traversal:\n%s", frame)
	}
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})

	frame := h.Frame()
	if !strings.Contains(frame, "new target · Kimi") || !strings.Contains(frame, "credential") {
		t.Fatalf("provider selection closed the inline target config:\n%s", frame)
	}
	if got := section.State.AddTargetRoute.Get(); got != route.ID {
		t.Fatalf("add target route = %q, want %q", got, route.ID)
	}
}
