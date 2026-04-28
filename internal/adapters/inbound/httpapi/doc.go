// Package httpapi implements the client-facing HTTP compatibility surface.
//
// It owns endpoint-qualified path splitting plus client-family codecs that
// decode requests and re-encode canonical outputs at the HTTP edge. It also
// owns HTTP rendering of minimal daemon operator control routes such as status,
// endpoint intent, model catalog, and compatibility model-discovery routes
// because transport shape belongs at the edge even when runtime truth is
// produced elsewhere. It must not take on provider-dialect logic or redefine
// canonical compatibility semantics locally.
package httpapi
