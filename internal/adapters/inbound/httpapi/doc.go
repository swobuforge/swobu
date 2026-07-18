// Package httpapi implements the client-facing HTTP protocol surface.
//
// It owns endpoint-qualified path splitting and transport framing at the HTTP
// edge. Protocol-family codecs are delegated to shared protocol codec packages.
// It also
// owns HTTP rendering of daemon operator control routes such as status,
// workspaces, model catalog, and protocol model-discovery routes. Workspace
// commands use method-aware http.ServeMux patterns and Request.PathValue.
// Transport shape belongs at the edge even when runtime truth is produced
// elsewhere. This package must not take on provider-dialect logic or redefine
// canonical request semantics.
package httpapi
