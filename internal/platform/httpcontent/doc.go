// Package httpcontent owns transport-level HTTP content-coding helpers used at
// the ingress and provider-execution edges.
//
// These helpers handle interoperability mechanics such as gzip, deflate, and
// zstd. They must not acquire prompt or token-compaction semantics.
package httpcontent
