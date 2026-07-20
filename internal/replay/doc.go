// Package replay owns explicit replay lookup and terminal-success commit.
//
// Replay is semantic request preparation plus terminal-success commit.
// Prepared state carries both complete semantics and the inherited current
// delta. Canonical request presence distinguishes omitted durable bands from
// explicit empty values: omission inherits, empty clears, and non-empty
// replaces. Exact routing target ID and process-local version equality selects
// an applicable typed Responses refinement after backend resolution.
// One daemon-global store partitions records by the validated workspace slug
// resolved from the request URL. Committed records carry a bounded expiry owned by the store, with the
// memory store applying the package default when a record arrives without one,
// so the daemon does not accumulate replay state forever.
// Materialized external media is normalized into owned byte assets plus
// position-bound URL occurrences. Identical assets are retained once; bindings
// preserve which exact historical request part used them. The canonical URL
// remains semantic source truth. Every provider attempt applies the durable
// binding and uses the exact inline bytes rather than refetching the URL.
//
// This package intentionally does NOT own:
//   - prefix matching
//   - chain walking
//   - provider-specific replay strategy
//   - replay planning
//   - reducer coordination
//   - transcript recognition or deduplication
//
// Prepare is one record deep. Terminal capture stores its already-materialized
// semantic request without chain walking or target taxonomies.
package replay
