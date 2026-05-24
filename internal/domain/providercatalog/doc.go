// Package providercatalog owns the canonical provider descriptor catalog used
// by domain validation and operator-facing setup surfaces, plus capability
// facts consumed by request-path policy orchestration.
//
// It also owns the auth-threading matrix contract that maps each declared
// provider auth mode to concrete credential threading semantics (env/file/
// interactive/AWS chain) used by runtime and proof lanes.
//
// Runtime adapter dispatch is owned by outbound provider composition, not by
// this catalog.
package providercatalog
