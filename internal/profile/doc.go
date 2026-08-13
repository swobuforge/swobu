// Package profile owns the canonical provider descriptor catalog used by
// domain validation and operator-facing setup surfaces.
//
// It owns stable locator, durable connection-shape, credential requirement and
// authoring, ambient/reference display labels, catalog-noun, and protocol facts. Credential storage sources,
// provider probe diagnostics, and ChatGPT login mechanics are deliberately
// outside this catalog; provider runtime authentication strategies belong to
// their outbound adapters.
// Bedrock endpoint resolution is the exception to static catalog-only data:
// ResolveBedrockEndpoint normalizes the required operator-authored inference
// endpoint and appends one protocol operation. BedrockCatalogURL independently
// projects canonical regional catalog connectivity. Region never supplies an
// inference namespace.
// Compatibility changes live outside this catalog and never drive routing.
// Routing construction boundaries obtain concrete provider/protocol, Azure endpoint,
// and Bedrock region predicates through RoutingConstructionFacts; adapters must not
// reconstruct that catalog mapping independently.
//
// Static manifest truth lives here.
//
// Runtime adapter dispatch is owned by outbound provider composition, not by
// this catalog. AmbientOrReference is shared authoring semantics only: provider
// adapters separately own identity discovery, lifecycle, signing or token
// acquisition, and request authentication. It is not authority for a generic
// cloud authenticator.
package profile
