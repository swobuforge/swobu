// Package replay owns explicit replay lookup and terminal-success commit.
//
// Replay is attempt-local request preparation plus terminal-success commit.
// Canonical request always flows forward. Native replay pointer is nullable.
// Nil means full canonical request. Present means native ref plus canonical delta.
// Committed records carry a bounded expiry owned by the store, with the
// memory store applying the package default when a record arrives without one,
// so the daemon does not accumulate replay state forever.
//
// This package intentionally does NOT own:
//   - prefix matching
//   - chain walking
//   - provider-specific replay strategy
//   - replay planning
//   - reducer coordination
//   - transcript recognition or deduplication
//
// The only two store-aware functions are Prepare and CaptureRequest.
// Both are one record deep and operate on exact TargetKey equality only.
package replay
