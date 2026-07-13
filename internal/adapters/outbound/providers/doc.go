// Package providers owns the outbound provider namespace registry and dispatch.
//
// It composes provider-local facets for manifest, discovery, and execution at
// the composition edge, resolves runtime bundles through registered provider
// factories, and keeps route-support policy in providercompat.
package providers
