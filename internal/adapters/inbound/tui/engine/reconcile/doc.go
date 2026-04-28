// Package reconcile owns private view-to-node reconciliation, stable
// identity recovery, and node-scoped local state retention for the retained TUI
// runtime.
//
// The package is organized around two core nouns:
//
//   - ViewRenderNode: the ephemeral declarative tree built from views and structural
//     nodes during one rebuild
//   - RetainedRenderNode: the durable runtime-owned identity tree that preserves local
//     state, focus continuity, and mount/unmount ownership across rebuilds
//
// Its core invariant is sibling-scoped continuity: explicit keys preserve
// retained NodeID ownership across reorder within one parent, while fixed
// structural children fall back to stable child hints that encode sibling
// position. Sibling keys must be unique within one parent. Fresh numeric IDs
// are monotonic and never reused.
package reconcile
