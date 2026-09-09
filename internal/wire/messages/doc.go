// Package messages maps canonical exchanges to Anthropic Messages.
//
// It owns family-level encoding and stream decoding. Authentication,
// transport, routing, and provider-specific capability decisions stay outside.
// Opaque provider content is not interpreted as readable message text.
package messages
