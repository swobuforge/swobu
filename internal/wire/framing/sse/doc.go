// Package sse owns shared JSON framing helpers used by wire adapters.
//
// It keeps permissive JSON-object decoding and permissive request-envelope
// decoding in one boundary package so request DTOs can log unexpected
// top-level fields, ignore them, and still surface pointer-bearing canonical
// errors for malformed or semantically invalid payloads.
package sse
