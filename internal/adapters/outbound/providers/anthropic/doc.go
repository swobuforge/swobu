// Package anthropic owns the shared x-api-key Messages transport used by
// Anthropic-compatible provider profiles.
//
// It owns Messages auth/header wiring, transport execution, and backend error
// truth preservation. Anthropic model discovery remains here; providers with a
// different discovery origin compose this transport with their own discovery.
package anthropic
