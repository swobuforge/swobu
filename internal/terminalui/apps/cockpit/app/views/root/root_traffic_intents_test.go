package root

import (
	"fmt"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/geom"
)

func TestRoot_TrafficRowsRemainNavigableAfterOpeningRow(t *testing.T) {
	t.Parallel()

	rt := newTestRuntime(state.Model{
		HeaderStatus:    "ready",
		DaemonState:     "up",
		Endpoints:       []string{"acme"},
		CurrentEndpoint: "acme",
		TrafficRows: []state.TrafficRow{
			{RequestID: "req-3", OperationFamily: "responses", Target: "backend-a", Result: "in_progress", StatusCode: 0, ObservedAt: "11:11:03"},
			{RequestID: "req-2", OperationFamily: "responses", Target: "backend-a", Result: "in_progress", StatusCode: 0, ObservedAt: "11:11:02"},
			{RequestID: "req-1", OperationFamily: "responses", Target: "backend-a", Result: "in_progress", StatusCode: 0, ObservedAt: "11:11:01"},
		},
	})
	viewport := geom.Rect{W: 120, H: 40}
	rt.Rebuild(Root(), viewport)

	focusRowContaining(t, rt, viewport, "traffic")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)

	focusRowContaining(t, rt, viewport, "11:11:03")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)

	rt.DispatchEvent(updateKey(interaction.KeyDown))
	rt.Rebuild(Root(), viewport)
	focusRowContaining(t, rt, viewport, "11:11:02")
}

func TestRoot_TrafficRows_WindowedListPreventsOverflow(t *testing.T) {
	t.Parallel()

	var trafficRows []state.TrafficRow
	for i := 1; i <= 12; i++ {
		id := fmt.Sprintf("req-%02d", i)
		when := fmt.Sprintf("11:22:%02d", i)
		trafficRows = append(trafficRows, state.TrafficRow{RequestID: id, OperationFamily: "responses",
			Target:     "backend-a",
			Result:     "in_progress",
			StatusCode: 0,
			ObservedAt: when,
		})
	}

	rt := newTestRuntime(state.Model{
		HeaderStatus:    "ready",
		DaemonState:     "up",
		Endpoints:       []string{"acme"},
		CurrentEndpoint: "acme",
		TrafficRows:     trafficRows,
	})
	viewport := geom.Rect{W: 120, H: 40}
	rt.Rebuild(Root(), viewport)

	focusRowContaining(t, rt, viewport, "traffic")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)

	out := rt.Render(viewport).String()
	const expectedWindow = 5
	if got := strings.Count(out, "11:22:"); got != expectedWindow {
		t.Fatalf("visible traffic rows=%d want %d; render=%q", got, expectedWindow, out)
	}
}

func TestRoot_TrafficRows_DownAtWindowEdgeScrollsWithinTrafficList(t *testing.T) {
	t.Parallel()

	var trafficRows []state.TrafficRow
	for i := 1; i <= 8; i++ {
		id := fmt.Sprintf("req-%02d", i)
		when := fmt.Sprintf("12:34:%02d", i)
		trafficRows = append(trafficRows, state.TrafficRow{RequestID: id, OperationFamily: "responses",
			Target:     "backend-a",
			Result:     "in_progress",
			StatusCode: 0,
			ObservedAt: when,
		})
	}

	rt := newTestRuntime(state.Model{
		HeaderStatus:    "ready",
		DaemonState:     "up",
		Endpoints:       []string{"acme"},
		CurrentEndpoint: "acme",
		TrafficRows:     trafficRows,
	})
	viewport := geom.Rect{W: 120, H: 40}
	rt.Rebuild(Root(), viewport)

	focusRowContaining(t, rt, viewport, "traffic")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)

	focusRowContaining(t, rt, viewport, "12:34:08")
	const windowRows = 5
	for i := 0; i < windowRows-1; i++ {
		rt.DispatchEvent(updateKey(interaction.KeyDown))
		rt.Rebuild(Root(), viewport)
	}
	focusRowContaining(t, rt, viewport, "12:34:04")

	rt.DispatchEvent(updateKey(interaction.KeyDown))
	rt.Rebuild(Root(), viewport)
	focusRowContaining(t, rt, viewport, "12:34:03")
}

func TestRoot_TrafficEmptyOpenRendersSummaryLineInsteadOfKVPadding(t *testing.T) {
	t.Parallel()

	rt := newTestRuntime(state.Model{
		HeaderStatus:    "ready",
		DaemonState:     "up",
		Endpoints:       []string{"acme"},
		CurrentEndpoint: "acme",
	})
	viewport := geom.Rect{W: 80, H: 24}
	rt.Rebuild(Root(), viewport)

	focusRowContaining(t, rt, viewport, "traffic")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)

	out := rt.Render(viewport).String()
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, "no traffic yet") {
			continue
		}
		leadingSpaces := len(line) - len(strings.TrimLeft(line, " "))
		if leadingSpaces > 6 {
			t.Fatalf("traffic empty line has key/value style padding, want summary indent: %q", line)
		}
		return
	}
	t.Fatalf("render missing no-traffic line: %q", out)
}
