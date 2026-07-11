// Package sse owns shared JSON framing helpers used by wire adapters.
//
// It keeps permissive JSON-object decoding and strict request-body decoding in
// one boundary package so request DTOs can fail closed on unknown fields while
// still surfacing pointer-bearing canonical errors.
package sse
