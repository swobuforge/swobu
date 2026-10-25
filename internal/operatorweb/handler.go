// Package operatorweb serves the generated local Web Operator from the Swobu
// binary. It owns asset routing and browser document policy, not semantics.
package operatorweb

import (
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
)

//go:embed assets/* assets/assets/*
var generated embed.FS

type Handler struct{ assets fs.FS }

func NewHandler() (Handler, error) {
	assets, err := fs.Sub(generated, "assets")
	if err != nil {
		return Handler{}, err
	}
	return Handler{assets: assets}, nil
}

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Unknown control and retired paths reach this fallback too. Classify the
	// path before the method so removal has ordinary 404 semantics for all verbs.
	if r.URL.Path != "/swobu.png" && !strings.HasPrefix(r.URL.Path, "/assets/") && !IsSPARoute(r.URL.EscapedPath()) {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/assets/") {
		h.serveAsset(w, r)
		return
	}
	if r.URL.Path == "/swobu.png" {
		h.serveLogo(w, r)
		return
	}
	h.serveIndex(w, r)
}

// IsSPARoute classifies escaped URL paths so a pattern containing / remains
// one browser route segment. It is the closed address-space predicate shared by
// the SPA fallback and the Workspace data-plane dispatcher. It MUST NOT match
// client-facing /workspaces/{slug}/v1, /mcp or /search endpoints.
func IsSPARoute(requestPath string) bool {
	if requestPath == "/" {
		return true
	}
	if !strings.HasPrefix(requestPath, "/") || strings.Contains(requestPath, "//") {
		return false
	}
	parts := strings.Split(strings.Trim(requestPath, "/"), "/")
	switch parts[0] {
	case "workspaces":
		if len(parts) == 1 {
			return true
		}
		if len(parts) < 3 || parts[1] == "" {
			return false
		}
		switch parts[2] {
		case "overview", "access", "limits", "connect":
			return len(parts) == 3
		case "services":
			if len(parts) == 3 {
				return true
			}
			if len(parts) == 4 {
				return parts[3] == "models" || parts[3] == "mcp" || parts[3] == "search"
			}
			return len(parts) == 5 && parts[3] == "models" && parts[4] != ""
		}
	case "connections", "access", "activity":
		return len(parts) == 1 || len(parts) == 2 && parts[1] != ""
	case "settings":
		return len(parts) == 2 && (parts[1] == "history" || parts[1] == "network" || parts[1] == "administration")
	}
	return false
}

func (h Handler) serveIndex(w http.ResponseWriter, r *http.Request) {
	setDocumentHeaders(w.Header())
	raw, err := fs.ReadFile(h.assets, "index.html")
	if err != nil {
		http.Error(w, "Web Operator is unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodGet {
		_, _ = w.Write(raw)
	}
}

func (h Handler) serveAsset(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if name == "assets" || !strings.HasPrefix(name, "assets/") {
		http.NotFound(w, r)
		return
	}
	raw, err := fs.ReadFile(h.assets, name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if contentType := mime.TypeByExtension(path.Ext(name)); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodGet {
		_, _ = w.Write(raw)
	}
}

// The accepted Swobu mark has a stable unversioned URL. Never cache it as
// immutable; unlike fingerprinted /assets/ content it can change on upgrade.
func (h Handler) serveLogo(w http.ResponseWriter, r *http.Request) {
	raw, err := fs.ReadFile(h.assets, "swobu.png")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodGet {
		_, _ = w.Write(raw)
	}
}

func setDocumentHeaders(header http.Header) {
	header.Set("Content-Security-Policy", "default-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'; object-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:")
	header.Set("Cross-Origin-Resource-Policy", "same-origin")
	header.Set("Referrer-Policy", "no-referrer")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("X-Frame-Options", "DENY")
}
