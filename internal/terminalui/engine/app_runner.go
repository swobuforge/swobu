package engine

import (
	"context"

	"github.com/gdamore/tcell/v2"

	"github.com/swobuforge/swobu/internal/terminalui/core"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/host"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/loop"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
)

// RunApp wires one typed core.App into the retained terminal runtime and
// blocks until the app exits.
//
// This is the canonical production entrypoint for the terminalui framework.
// Callers supply only:
//   - a tcell screen
//   - a core.App[State, Event] implementing state/view/effects
//
// The retained engine (loop, rendergraph, host) is internal implementation
// detail; app code must not depend on it. No retained types appear in the
// public API.
func RunApp[State any, Event any](
	ctx context.Context,
	screen tcell.Screen,
	app core.App[State, Event],
) error {
	// The event caster wraps typed events into the retained action envelope
	// internally. This is a pure bridge detail; callers never see it.
	caster := func(event Event) update.Action {
		return update.TypedAction[Event]{Event: event}
	}

	appLoop := loop.NewCoreAppLoop(app, caster)
	viewSpec := loop.CoreViewSpec(app, caster)

	runner := host.New(screen, viewSpec, appLoop.Model, appLoop.Reduce)
	runner.Loop = appLoop

	return runner.Run(ctx)
}
