// Package routing owns route resolution, attempt plan construction, monotonic
// execution walk, failure classification, cooldown management, and trace
// emission.
//
// Routing is a workspace-owned bounded context. It sits above the exchange
// runner and below the client ingress decode/encode layer.
//
// The canonical model lives in docs/03-architecture/system-shape-and-request-flow/monotonic-routing-boundary-and-attempt-semantics.md
// and docs/02-domain/glossary/canonical-terms.md.
//
// Boundary rules:
//   - No dependency on internal/exchange.
//   - No dependency on provider catalog (internal/profile).
//   - Minimal dependency on internal/delivery (RequestFacts may use streaming bool).
//   - canonical.CanonicalRequest is the only request shape consumed.
package routing
