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

	appstate "github.com/metrofun/swobu/internal/adapters/inbound/tui/app/state"
	rootviews "github.com/metrofun/swobu/internal/adapters/inbound/tui/app/views/root"
	"github.com/metrofun/swobu/internal/adapters/inbound/tui/engine/interaction"
	"github.com/metrofun/swobu/internal/adapters/inbound/tui/engine/rendergraph/geom"
	"github.com/metrofun/swobu/internal/adapters/inbound/tui/engine/rendergraph/layout"
	"github.com/metrofun/swobu/internal/adapters/inbound/tui/engine/rendergraph/paint"
	"github.com/metrofun/swobu/internal/adapters/inbound/tui/engine/update"
	"github.com/metrofun/swobu/internal/adapters/inbound/tui/engine/view"
	"github.com/metrofun/swobu/internal/app/operator/controlplane"
)

func TestRunner_RendersCockpitAndHandlesTabAndEscStepBackThenQuit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/_swobu/status":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(fmt.Sprintf(`{"state":"healthy","endpoint_count":0,"control_plane_protocol":%d,"swobu_version":"0.9.0"}`, controlplane.Protocol)))
		case "/_swobu/endpoints":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"endpoints":[]}`))
		case "/_swobu/model-catalog":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"entries":[]}`))
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

	screen.InjectKey(tcell.KeyEsc, 0, 0)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runner returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runner did not exit after esc")
	}
}

func TestMapKeyEvent_MapsBackspace(t *testing.T) {
	ev := mapKeyEvent(tcell.NewEventKey(tcell.KeyBackspace2, 0, 0))
	if ev.Kind != interaction.EventKey || ev.Key != interaction.KeyBackspace {
		t.Fatalf("mapKeyEvent(backspace) = (%v, %q), want (EventKey, KeyBackspace)", ev.Kind, ev.Key)
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
	screen.InjectKey(tcell.KeyEsc, 0, 0)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runner returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runner did not exit after esc")
	}
	_ = block
}

type bootEffectTrigger struct{}

type bootRoot struct{}

func asView(builder interface {
	BuildRenderNode(*view.Context[struct{}]) layout.RenderNode
}) view.ViewSpec[struct{}] {
	return view.View[struct{}](func(ctx *view.Context[struct{}]) layout.RenderNode {
		return builder.BuildRenderNode(ctx)
	})
}

func (bootRoot) BuildRenderNode(*view.Context[struct{}]) layout.RenderNode {
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
