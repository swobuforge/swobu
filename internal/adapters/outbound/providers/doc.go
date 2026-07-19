// Package providers owns the outbound provider namespace registry and dispatch.
//
// It explicitly composes provider-local facets for manifest, discovery, and
// execution at the composition edge. Provider availability never depends on
// package initialization or mutable global registration.
package providers
