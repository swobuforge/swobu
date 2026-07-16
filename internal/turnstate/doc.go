// Package turnstate owns opaque provider-native replay state.
//
// Semantic playback lives in replay records. TurnState stores provider-native
// bytes and handle fragments that may be replayed when a backend supports
// native continuation. That separation keeps provider-native mechanics out of
// the semantic request model.
//
// DEPRECATED: This package is a legacy namespace. The canonical continuation
// runtime and continuation store have been deleted (PR-6). Any remaining
// symbols should migrate to internal/replay in future slices.
package turnstate
