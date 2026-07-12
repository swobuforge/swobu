// Package view defines retained interactive view composition primitives under
// the shared terminalui/view namespace.
//
// Keep import usage explicit:
//   - internal/terminalui/component: author-facing semantic component API
//     that builds core.Node values
//   - internal/terminalui/components/*: reusable primitive/compound semantic
//     component constructors
//   - internal/terminalui/transcript: canonical transcript view specs and
//     render-mode semantics
//   - internal/terminalui/view: deprecated compatibility shim during quarantine
//   - internal/terminalui/view/retained: retained interactive composition API,
//     local state and memo/effect hooks, and local event ownership hooks
//
// RenderNode is still a compatibility alias here; new app-facing code should
// target component/core nodes and lowering adapters instead of rendergraph
// internals. FromCore remains the retained bridge during migration.
package retained
