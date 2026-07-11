// Package messages translates canonical semantic requests and outputs to/from
// Anthropic-style messages wire shapes.
//
// It owns only protocol-edge mapping. Provider auth, base URL, and transport
// behavior stay in provider wiring packages.
package messages
