// Package protocols contains low-level protocol-edge helpers shared by outbound
// protocol adapters.
//
// It may host generic SSE or wire-request utilities, but canonical semantic
// decisions must stay in compatibility and concrete protocol mapping must stay
// in the protocol-specific subpackages.
package protocols
