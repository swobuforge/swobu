// Package session owns immutable state required to resume a logical model
// session across independent requests.
//
// Each successful response commits one checkpoint keyed by its Swobu response
// ID and partitioned by workspace. A later request may resume from that exact
// checkpoint. Resolution produces both the complete canonical request and,
// when the checkpoint contains a valid exact-target provider refinement, a
// native-resumption delta.
//
// Checkpoints also retain validated external-media bytes bound to exact
// canonical request occurrences, so resumed execution never depends on
// refetching mutable URLs.
//
// Package session does not own routing, provider selection, response-ID
// allocation, mutable session heads, authentication, or transport encoding.
package session
