// Package profile owns the canonical provider descriptor catalog used by
// domain validation and operator-facing setup surfaces.
//
// It owns stable locator, credential-requirement, catalog-noun, and protocol
// facts. Credential storage sources, provider probe diagnostics, and ChatGPT
// login methods are deliberately outside this catalog; Bedrock runtime
// authentication strategy belongs to the Bedrock adapter.
// Compatibility decisions live outside this catalog and never drive routing.
// Routing construction boundaries obtain concrete provider/protocol, Azure endpoint,
// and Bedrock region predicates through RoutingConstructionFacts; adapters must not
// reconstruct that catalog mapping independently.
//
// Static manifest truth still lives here until the provider namespace registry
// fully rehomes those declarations.
//
// Runtime adapter dispatch is owned by outbound provider composition, not by
// this catalog.
package profile
