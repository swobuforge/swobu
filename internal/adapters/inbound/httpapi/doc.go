// Package httpapi implements the client-facing HTTP protocol surface.
//
// It owns endpoint-qualified path splitting and transport framing at the HTTP
// edge. Protocol-family codecs are delegated to shared protocol codec packages.
// It also
// owns HTTP rendering of minimal daemon operator control routes such as status,
// endpoint intent, model catalog, and protocol model-discovery routes
// because transport shape belongs at the edge even when runtime truth is
// produced elsewhere. It must not take on provider-dialect logic or redefine
// canonical request semantics locally.
package httpapi
