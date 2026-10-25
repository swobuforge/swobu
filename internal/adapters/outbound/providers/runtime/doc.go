// Package runtime owns provider-facet role contracts and runtime bundle wiring
// for the outbound provider namespace.
//
// It defines the live provider execution and discovery roles used by the
// provider composition, while provider packages own the concrete
// implementations assembled at the namespace edge. ResolveTargetCredential
// preserves captured Connection ownership across execution and discovery;
// a bound snapshot requires scoped resolution and never falls back to provider
// material. Providers with no auth or ambient identity retain those policies.
package runtime
