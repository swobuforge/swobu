// Package view defines the renderer-agnostic declarative terminal view tree.
//
// It is intentionally minimal: composition structure + line retention semantics
// + render mode intent. Rendering strategies (append/live/fullscreen/retained)
// consume this contract but are implemented elsewhere.
package view
