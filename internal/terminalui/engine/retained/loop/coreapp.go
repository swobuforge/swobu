// swobu:lint ignore test-only-dead-cluster because=typed core.App bridge is staged until batch-2 cockpit runtime wiring makes it production-rooted.
// TODO(batch2): route the production cockpit runner through core.App and delete this bridge suppression.
// CoreAppLoop wires a typed core.App into the retained engine.
//
// This adapter translates between:
//
//	core.App[S, E] → retained loop.Reducer[S] (dispatch side)
//	core.Effect[E]  → retained update.Effect   (execution side)
//	core.Node[E]    → layout.RenderNode        (render side via corelower)
//
// The retained engine owns terminal I/O, layout, and focus. The app owns
// state, view, and semantic meaning. This seam must not leak app code.
package loop

import (
	"context"

	"github.com/swobuforge/swobu/internal/terminalui/core"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/corelower"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

// swobu:lint ignore test-only-dead-cluster because=typed core.App bridge is staged until batch-2 cockpit runtime wiring makes it production-rooted.
// CoreAppReducer bridges a typed core.App into the retained Reducer[M]
// contract. The engine dispatches update.Action; this adapter asserts to
// Event safely because every action was emitted by a typed signal or effect.
func CoreAppReducer[Model any, Event any](app core.App[Model, Event]) Reducer[Model] {
	return func(model *Model, action update.Action) []update.Effect {
		event, ok := action.(Event)
		if !ok {
			return nil
		}
		newModel, effects := app.Update(*model, event)
		*model = newModel
		return bridgeEffects(effects)
	}
}

// CoreViewSpec builds one retained-style ViewSpec from the core.App View
// method. The lowerer bridges core.Node[E] down to layout.RenderNode.
func CoreViewSpec[Model any, Event any](
	app core.App[Model, Event],
	caster corelower.EventCaster[Event],
) retained.ViewSpec[Model] {
	return retained.View[Model](func(ctx *retained.Context[Model]) retained.RenderNode {
		model := ctx.Model()
		node := app.View(model)
		render, err := corelower.Lower(node, corelower.EnvConfig{}, caster)
		if err != nil {
			return nil
		}
		return render
	})
}

func bridgeEffects[E any](effects []core.Effect[E]) []update.Effect {
	if len(effects) == 0 {
		return nil
	}
	out := make([]update.Effect, 0, len(effects))
	for _, eff := range effects {
		out = append(out, coreEffectBridge[E]{eff})
	}
	return out
}

// coreEffectBridge wraps one core.Effect[E] into a retained update.Effect.
// The runtime executes Effect.Execute, which bridges back into typed
// events via follow-up dispatch.
type coreEffectBridge[E any] struct {
	core.Effect[E]
}

func (b coreEffectBridge[E]) Execute(ctx context.Context) []update.Action {
	if b.Run == nil {
		return nil
	}
	result := b.Run(ctx)
	return []update.Action{result}
}

// swobu:lint ignore test-only-dead-cluster because=typed core.App bridge is staged until batch-2 cockpit runtime wiring makes it production-rooted.
// LowerCoreNode is the engine-side bridge that turns one semantic node tree
// into a retained rendergraph. Exported so the runtime can call it on the
// top-level view node.
func LowerCoreNode[E any](node core.Node[E], caster corelower.EventCaster[E]) (layout.RenderNode, error) {
	render, err := corelower.Lower(node, corelower.EnvConfig{}, caster)
	if err != nil {
		return nil, err
	}
	return render, nil
}

// NewCoreAppLoop wires one core.App into a retained AppLoop ready for
// host.Runner. The caller supplies the screen, the typed app, and the
// event caster that knows how to translate typed events into update.Action
// for any retained-internal dispatch that still routes through the untyped
// path (focus/lifecycle runtime actions).
func NewCoreAppLoop[Model any, Event any](
	app core.App[Model, Event],
	caster corelower.EventCaster[Event],
) *AppLoop[Model] {
	model, initEffects := app.Init()
	reduce := CoreAppReducer[Model, Event](app)
	loop := New(model, reduce)
	// Execute any init effects through the bridge.
	for _, eff := range bridgeEffects(initEffects) {
		loop.executeEffect(eff)
	}
	return loop
}
