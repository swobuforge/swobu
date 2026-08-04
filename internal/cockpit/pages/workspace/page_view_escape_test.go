package workspace

import (
	"context"
	"strings"
	"testing"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
)

func TestPage_EscapeCollapsesExpandedRouteBeforeStoppingCockpit(t *testing.T) {
	page := Page(readmodel.WorkspaceReadModel{
		ID:    "dev",
		Slug:  "dev",
		State: readmodel.WorkspaceExisting,
		Routes: []readmodel.RouteReadModel{{
			ID:        "azure",
			ModelName: "azure",
			Enabled:   true,
		}},
	}, nil, nil, nil, context.Background(), nil, nil)
	page.RoutesSection.State.ExpandedRoute.Set("azure")

	h, err := testkit.NewHarness(page)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})

	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEscape})

	if got := page.RoutesSection.State.ExpandedRoute.Get(); got != "" {
		t.Fatalf("expanded route after Escape = %q, want collapsed", got)
	}
	frame := h.Frame()
	if !strings.Contains(frame, "expand ↵") {
		t.Fatalf("route row after Escape must render collapsed state, got:\n%s", frame)
	}
}
