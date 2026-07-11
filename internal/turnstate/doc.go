// Package turnstate owns opaque provider-native replay state.
//
// Semantic continuation lives in canonical continuation records. TurnState
// stores provider-native bytes and handle fragments that may be replayed when a
// backend supports native continuation. That separation keeps provider-native
// mechanics out of the semantic request model.
package turnstate
