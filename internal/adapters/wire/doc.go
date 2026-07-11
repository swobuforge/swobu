// Package wire defines the transport-to-canonical boundary.
//
// Naming grammar:
// - request_encode.go: canonical request -> wire request payload.
// - request_decode.go: wire request payload -> canonical request.
// - response_decode.go: wire response payload/stream -> canonical output/events.
// - codec.go: protocol-level facade implementing runtime codec contracts.
// - wire_dto.go: protocol wire DTO schemas.
//
// Provider-specific behavior is not implemented in protocol packages.
// Protocol packages own only shared protocol syntax/shape.
package wire
