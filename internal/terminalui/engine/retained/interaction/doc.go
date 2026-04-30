// Package interaction defines optional retained-runtime capabilities layered on
// top of the structural TUI engine: events, hit testing, focus, and lifecycle.
//
// # Orthogonal interaction aspects
//
// Each interface in this package represents one independent capability. Render nodes
// implement only the aspects they need:
//
//   - [Hittable]: participates in mouse hit testing
//   - [EventHandler]: processes keyboard and mouse events
//   - [ScopedEventHandler]: processes events with explicit bubbling semantics
//   - [Focusable]: can receive focus during navigation
//   - [FocusEvents]: observes focus enter/exit transitions
//   - [Lifecycle]: observes mount/unmount transitions
//
// The [Event] type carries typed [Key] enums rather than string literals.
// Terminal adapters map platform-specific input into these typed values.
//
// # Toolkit primitives as adapters
//
// Toolkit primitives like Action, Input, and ChoiceList are adapters that
// compose these aspects for specific interaction patterns. They are NOT the
// interaction model center -- the model is defined by the orthogonal interfaces
// above. New interaction patterns should compose from these aspects rather than
// extending shape-specific primitives.
package interaction
