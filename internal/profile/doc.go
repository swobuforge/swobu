// Package profile owns the canonical provider descriptor catalog used by
// domain validation and operator-facing setup surfaces.
//
// It also owns the auth-threading matrix contract and route-resolution helpers
// that map each declared provider auth mode to concrete credential threading
// semantics (env/file/interactive/AWS chain) used by runtime and proof lanes.
// Route capability facts for request features now live in internal/compat so
// provider dispatch can consult one shared vocabulary.
// Routing construction boundaries obtain provider/protocol, Azure endpoint,
// and Bedrock region predicates through RoutingCapabilities; adapters must not
// reconstruct that catalog mapping independently.
//
// Static manifest truth still lives here until the provider namespace registry
// fully rehomes those declarations.
//
// Runtime adapter dispatch is owned by outbound provider composition, not by
// this catalog.
package profile
