// Package compiler compiles a semantic Node[E] tree into a terminal Frame,
// along with diagnostics, layout, focus graph, style table, and interaction routes.
//
// The compiler is a pure package: no I/O, no goroutines, no terminal state.
// It depends only on internal/tui/core.
//
// Pass order (each pass is deterministic and effect-free):
//
//   1. Normalize   – fill defaults (auto-Key, auto-FocusID), build parent links.
//   2. Validate    – core semantic validation plus compiler-level layout/focus rules.
//   3. ResolveStyle – role + visual state + capabilities → stable StyleID.
//   4. ResolveLayout – Fit/Fixed/Fill/Range + overflow → rectangles.
//   5. BuildFocusGraph – visible focus targets, scope membership, enabled status.
//   6. BuildInteractionRoutes – scoped routes + global shortcuts → route table.
//   7. Paint         – layout rectangles + styles → cell frame.
//
// Paint is the first pass allowed to think about terminal cells.
// No terminal output before validation passes.
//
// Ownership: Runtime/TUI Runtime (compiler pass contracts and deterministic output).
package compiler
