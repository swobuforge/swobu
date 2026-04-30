// Package loop owns the retained app loop over the structural TUI engine:
// model reduction, effect execution, rebuild invalidation, focus state, and
// event dispatch.
//
// Event dispatch is structural and product-agnostic. Input targets a retained
// node, then propagates toward ancestors. The nearest ScopedEventHandler may
// consume; otherwise the event bubbles. Runtime never invents cockpit product
// semantics or fallback intents.
//
// Focus traversal is expressed via scoped handlers that emit engine-level
// interaction actions (for example FocusMoveAction). Runtime ships no implicit
// key-navigation defaults.
//
// Internal breakdown:
//   - rebuild_coordinator: reconcile + retained-tree lifecycle
//   - effect_runner: reducer/effect execution and follow-up delivery
//   - event_router: hit-test targeting and scoped bubbling
//   - focus_manager: focus transitions, traversal, and repair
//   - render_painter: retained tree paint walk
package loop
