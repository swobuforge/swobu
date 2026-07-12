//go:build !race

package host

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/swobuforge/swobu/internal/app/operator/controlplane"
	appstate "github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	rootviews "github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/views/root"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/paint"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	toolkitviews "github.com/swobuforge/swobu/internal/terminalui/toolkit/views"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

func TestRunner_RendersCockpitAndIgnoresEscThenQuitsOnCtrlC(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/_swobu/status":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(fmt.Sprintf(`{"state":"healthy","endpoint_count":0,"control_plane_protocol":%d,"swobu_version":"0.9.0"}`, controlplane.Protocol)))
		case "/_swobu/endpoints":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"endpoints":[]}`))
		case "/_swobu/status-projection":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"scope":{"kind":"all"},"recent_traffic":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	t.Setenv("SWOBU_DAEMON_URL", srv.URL)

	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)

	runner := New(screen, rootviews.Root(), appstate.Model{
		HeaderStatus: "ready",
		DaemonState:  "up",
	}, appstate.Reduce)

	done := make(chan error, 1)
	go func() {
		done <- runner.Run(context.Background())
	}()

	waitFor(t, screen, done, func() bool {
		s := screenString(screen)
		return strings.Contains(s, "workspace") && strings.Contains(s, ">")
	})

	screen.InjectKey(tcell.KeyEsc, 0, 0)
	time.Sleep(100 * time.Millisecond)
	select {
	case err := <-done:
		t.Fatalf("runner exited on first esc, want step-back behavior: %v", err)
	default:
	}

	screen.InjectKey(tcell.KeyCtrlC, 0, 0)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runner returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runner did not exit after ctrl+c")
	}
}

func TestMapKeyEvent_MapsBackspace(t *testing.T) {
	ev := mapKeyEvent(tcell.NewEventKey(tcell.KeyBackspace2, 0, 0))
	if ev.Kind != interaction.EventKey || ev.Key != interaction.KeyBackspace {
		t.Fatalf("mapKeyEvent(backspace) = (%v, %q), want (EventKey, KeyBackspace)", ev.Kind, ev.Key)
	}
}

func TestMapKeyEvent_MapsReturnRuneToEnter(t *testing.T) {
	for _, evIn := range []*tcell.EventKey{
		tcell.NewEventKey(tcell.KeyEnter, 0, 0),
		tcell.NewEventKey(tcell.KeyCtrlJ, 0, 0),
		tcell.NewEventKey(tcell.KeyCtrlM, 0, 0),
	} {
		ev := mapKeyEvent(evIn)
		if ev.Kind != interaction.EventKey || ev.Key != interaction.KeyEnter {
			t.Fatalf("mapKeyEvent(%q) = (%v, %q), want (EventKey, KeyEnter)", evIn.Name(), ev.Kind, ev.Key)
		}
	}
}

func TestRunner_MouseButtonNoneDoesNotFocusOrActivate(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(20, 4)

	activations := []string{}
	root := retained.View[struct{}](func(ctx *retained.Context[struct{}]) layout.RenderNode {
		first := toolkitviews.NewAction(8, true, false, func(bool, int) string {
			return "first"
		}, func(trigger string) []update.Action {
			activations = append(activations, "first:"+trigger)
			return nil
		}, nil)
		second := toolkitviews.NewAction(8, true, false, func(bool, int) string {
			return "second"
		}, func(trigger string) []update.Action {
			activations = append(activations, "second:"+trigger)
			return nil
		}, nil)
		return layout.NewColumn(
			layout.FlowChild{RenderNode: first},
			layout.FlowChild{RenderNode: second},
		)
	})

	runner := New(screen, root, struct{}{}, func(*struct{}, update.Action) []update.Effect {
		return nil
	})
	runner.Loop.Rebuild(root, geom.Rect{W: 20, H: 4})

	tree := runner.Loop.Tree
	kids := 0
	if tree != nil {
		kids = len(tree.Kids)
	}
	if tree == nil || kids != 2 {
		t.Fatalf("tree kids = %d, want 2", kids)
	}
	first := tree.Kids[0]
	second := tree.Kids[1]
	if runner.Loop.Focused != first {
		t.Fatalf("focused = %v, want first row", runner.Loop.Focused)
	}

	runner.handleEvent(tcell.NewEventMouse(0, 1, tcell.ButtonNone, 0))
	if runner.Loop.Focused != first {
		t.Fatalf("focused after hover-like mouse event = %v, want first row", runner.Loop.Focused)
	}
	if len(activations) != 0 {
		t.Fatalf("activations after hover-like mouse event = %v, want none", activations)
	}

	runner.handleEvent(tcell.NewEventMouse(0, 1, tcell.Button1, 0))
	if runner.Loop.Focused != second {
		t.Fatalf("focused after click = %v, want second row", runner.Loop.Focused)
	}
	if got, want := len(activations), 1; got != want {
		t.Fatalf("activation count after click = %d, want 1", got)
	}
	if got, want := activations[0], "second:mouse"; got != want {
		t.Fatalf("activation after click = %q, want %q", got, want)
	}
}

func TestRunner_FlushesFirstFrameBeforeBlockingBootEffect(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(40, 6)

	block := make(chan struct{})
	runner := New(screen, asView(bootRoot{}), struct{}{}, func(*struct{}, update.Action) []update.Effect {
		return nil
	})

	done := make(chan error, 1)
	go func() {
		done <- runner.Run(context.Background())
	}()

	waitFor(t, screen, done, func() bool {
		return strings.Contains(screenString(screen), "boot frame")
	})

	close(block)
	screen.InjectKey(tcell.KeyCtrlC, 0, 0)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runner returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runner did not exit after ctrl+c")
	}
	_ = block
}

func TestRunner_EnterOnClientsSectionFocusesClientRow(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(100, 28)

	runner := New(screen, rootviews.Root(), appstate.Model{
		HeaderStatus:    "ready",
		DaemonState:     "up",
		Endpoints:       []string{"acme"},
		CurrentEndpoint: "acme",
		EndpointSnapshots: []appstate.EndpointSnapshot{{
			Name:                      "acme",
			SelectedProviderConfigRef: "backend-a",
			ProviderConfigs: []appstate.ProviderConfigSnapshot{{
				Ref:          "backend-a",
				ProviderSpec: "openai_compatible",
				ModelID:      "gpt-4.1-mini",
			}},
		}},
	}, appstate.Reduce)

	done := make(chan error, 1)
	go func() {
		done <- runner.Run(context.Background())
	}()

	waitFor(t, screen, done, func() bool {
		return strings.Contains(screenString(screen), "workspace")
	})

	for i := 0; i < 12; i++ {
		if strings.Contains(screenString(screen), "> clients ▸") {
			break
		}
		screen.InjectKey(tcell.KeyDown, 0, 0)
		time.Sleep(30 * time.Millisecond)
	}
	if !strings.Contains(screenString(screen), "> clients ▸") {
		t.Fatalf("failed to focus clients header before open; screen=%q", screenString(screen))
	}

	screen.InjectKey(tcell.KeyEnter, 0, 0)
	waitFor(t, screen, done, func() bool {
		s := screenString(screen)
		return strings.Contains(s, "clients ▾") && strings.Contains(s, ">    client")
	})

	screen.InjectKey(tcell.KeyCtrlC, 0, 0)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runner returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runner did not exit after ctrl+c")
	}
}

type bootEffectTrigger struct{}

type bootRoot struct{}

func asView(builder interface {
	BuildRenderNode(*retained.Context[struct{}]) layout.RenderNode
}) retained.ViewSpec[struct{}] {
	return retained.View[struct{}](func(ctx *retained.Context[struct{}]) layout.RenderNode {
		return builder.BuildRenderNode(ctx)
	})
}

func (bootRoot) BuildRenderNode(*retained.Context[struct{}]) layout.RenderNode {
	return bootLeaf{}
}

type bootLeaf struct{}

func (bootLeaf) Measure(c geom.Constraints, _ *layout.LayoutContext) geom.Size {
	return geom.ClampSize(geom.Size{W: 10, H: 1}, c)
}

func (bootLeaf) Arrange(node *layout.LayoutNode, _ *layout.LayoutContext) layout.NodeLayout {
	return layout.NodeLayout{
		BorderRect:   node.Slot,
		ContentRect:  node.Slot,
		ViewportRect: node.Slot,
		ContentSize:  geom.Size{W: node.Slot.W, H: node.Slot.H},
	}
}

func (bootLeaf) Paint(p paint.Painter, _ *layout.LayoutNode, _ *layout.PaintContext) {
	p.Text(0, 0, "boot frame")
}

func (bootLeaf) OnMount(*layout.LayoutNode) []update.Action {
	return []update.Action{bootEffectTrigger{}}
}

func (bootLeaf) OnUnmount(*layout.LayoutNode) []update.Action {
	return nil
}

func waitFor(t *testing.T, screen tcell.SimulationScreen, done <-chan error, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			t.Fatalf("runner exited early: %v; screen=%q", err, screenString(screen))
		default:
		}
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("condition not met before timeout; screen=%q", screenString(screen))
}

func screenString(screen tcell.SimulationScreen) string {
	cells, width, height := screen.GetContents()
	lines := make([]string, height)
	for y := 0; y < height; y++ {
		var sb strings.Builder
		for x := 0; x < width; x++ {
			cell := cells[y*width+x]
			if len(cell.Runes) == 0 {
				sb.WriteRune(' ')
				continue
			}
			sb.WriteRune(cell.Runes[0])
		}
		lines[y] = strings.TrimRight(sb.String(), " ")
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}
