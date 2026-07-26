// Package session owns immutable state required to resume a logical model
// session across independent requests.
//
// Each successfully client-encoded response must commit one checkpoint keyed by
// its Swobu response ID and partitioned by workspace. Client wire storage hints
// cannot suppress this correctness state: provider-specific opaque thinking and
// resolved media may be absent from the client projection but required by a
// later continuation. An optional opaque
// client-history fingerprint provides one exact secondary lookup. Resolution
// materializes canonical checkpoint truth plus the codec-rebased current
// invocation, restoring opaque thinking hidden from client projection. It may also
// build a native-resumption delta when the matched checkpoint contains a valid
// exact-target provider handle. Reasoning controls and effort normally remain
// local to each invocation. When matching tool results continue an unfinished
// assistant turn, omitted compute and effort are resolved from the
// checkpoint request directly into the effective Full and Delta requests, and
// missing request-scoped directives and declarations are repeated without
// becoming history. They expire once the turn completes. Explicit compute or
// effort conflicts reject. Partial result batches are legal, while duplicate
// or foreign call IDs are malformed continuations.
//
// Checkpoints also retain validated external-media bytes bound to exact
// canonical request occurrences, so resumed execution never depends on
// refetching mutable URLs.
//
// Package session does not own history fingerprint composition, protocol DTO
// interpretation, client stream lifecycle, routing, provider selection,
// response-ID allocation, mutable session heads, authentication, or transport
// encoding.
package session
