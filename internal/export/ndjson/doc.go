// Package ndjson exports immutable CompletedExchange projections as the
// versioned Swobu execution-history NDJSON contract. It is independent of the
// Ledger: a configured file receives live completions directly and may operate
// while durable history is disabled or unavailable.
package ndjson
