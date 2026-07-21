// Package providers owns the outbound provider namespace registry and dispatch.
//
// It explicitly composes provider-local facets for manifest, discovery, and
// execution at the composition edge. Provider availability never depends on
// package initialization or mutable global registration.
//
// Provider-hosted web search follows the same edge ownership. Shared Responses
// and Messages codecs lower typed neutral request documents; exact provider
// codecs compose the final vendor spelling before the protocol performs the
// single JSON serialization, or reject locally with provider.UnsupportedByBackend.
// OpenAI, ChatGPT, and Azure use the standard Responses marker; Anthropic and
// Azure own separate Messages version literals, and OpenRouter owns its
// Responses spelling.
// Bedrock and Ollama reject; custom targets accept only the standard Responses
// marker. This is typed encode-time composition, not serialized JSON mutation,
// a preflight, capability registry, planner, or compatibility-controlled route.
package providers
