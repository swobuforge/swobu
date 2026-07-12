// Package transcript defines the renderer-agnostic declarative terminal tree
// for line-oriented output.
//
// It is intentionally minimal: composition structure + line retention
// semantics + render mode intent. Rendering strategies consume this contract
// but are implemented elsewhere.
//
// New code should import transcript directly.
package transcript
