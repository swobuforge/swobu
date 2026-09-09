// Package httpapi serves Swobu HTTP endpoints.
//
// This edge owns path splitting, transport framing, and bounded operator JSON
// envelopes; protocol codecs and application services own request semantics.
// Operator JSON accepts additive members but rejects malformed known fields,
// non-object bodies, and trailing values. Durable configuration remains a
// separate closed schema.
//
// Responses WebSocket upgrades require a loopback TCP peer, literal loopback
// authority, and exact browser origin; native clients may omit Origin.
// Forwarded headers are not trust inputs. Disconnect cancels the connection
// context, and exchanges run serially with distinct identities. Stream reads
// yield protocol messages, never arbitrary byte chunks treated as messages.
//
// Backend failures are projected into the ingress protocol's JSON error
// contract. Recognized structured provider detail may preserve its message;
// opaque bodies are never mislabeled or exposed as client-native structure.
// Ordinary request-outcome logs contain metadata only, not response bodies.
package httpapi
