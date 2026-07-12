// Package transcript defines the renderer-agnostic declarative terminal tree
// for line-oriented output.
//
// It is intentionally minimal: composition structure + line retention
// semantics + render mode intent. Rendering strategies consume this contract
// but are implemented elsewhere.
//
// Deprecated compatibility aliases remain in internal/terminalui/view during
// the quarantine window. New code should import transcript directly.
package transcript
