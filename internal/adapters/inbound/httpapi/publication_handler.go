package httpapi

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"

	publicationapp "github.com/swobuforge/swobu/internal/app/operator/publication"
	"github.com/swobuforge/swobu/internal/publications"
	"github.com/swobuforge/swobu/internal/routing"
)

var strongPublicationETag = regexp.MustCompile(`^"[0-9a-f]{64}"$`)

// PublicationHandler controls the built-in relay reachability of a Workspace.
// It does not own Services, credentials or network provisioning.
type PublicationHandler struct {
	Store          *publications.Store
	Exists         func(routing.WorkspaceSlug) bool
	Ready          func(context.Context) (func(), error)
	StopIfInactive func()
	RelayURL       func() string
}

func (h PublicationHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !IsLocalOperatorRequest(r) {
		http.Error(w, "local Node operator required", http.StatusForbidden)
		return
	}
	if h.Store == nil {
		http.Error(w, "publication unavailable", http.StatusServiceUnavailable)
		return
	}
	if r.URL.RawPath != "" || strings.Contains(r.URL.Path, "%") {
		http.NotFound(w, r)
		return
	}
	if r.URL.Path == "/api/v1/remote-addresses" {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		writeWorkspaceJSON(w, http.StatusOK, []map[string]any{{"id": "relay", "url": h.relayURL(), "managed": true}})
		return
	}
	slug, err := routing.ParseWorkspaceSlug(r.PathValue("workspace"))
	if err != nil || slug.String() != r.PathValue("workspace") {
		http.NotFound(w, r)
		return
	}
	if h.Exists == nil || !h.Exists(slug) {
		http.NotFound(w, r)
		return
	}
	if r.URL.RawQuery != "" {
		http.Error(w, "unexpected query", http.StatusBadRequest)
		return
	}
	if !strings.HasSuffix(r.URL.Path, "/publications/relay") {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		h.read(w, r, slug)
		return
	}
	if r.Method != http.MethodPut && r.Method != http.MethodDelete {
		w.Header().Set("Allow", "PUT, DELETE")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if r.ContentLength != 0 {
		http.Error(w, "publication mutation does not take a body", http.StatusBadRequest)
		return
	}
	if value := r.Header.Get("If-Match"); !strongPublicationETag.MatchString(value) {
		http.Error(w, "strong If-Match revision required", http.StatusPreconditionRequired)
		return
	}
	expected := strings.Trim(r.Header.Get("If-Match"), `"`)
	value, err := h.commands().Set(r.Context(), slug, r.Method == http.MethodPut, expected, func(context.Context) error { return permissionTransactionAdmission(r)() })
	if err != nil {
		switch {
		case errors.Is(err, publications.ErrConflict):
			http.Error(w, "publication changed", http.StatusPreconditionFailed)
		case errors.Is(err, publicationapp.ErrUnavailable):
			http.Error(w, "relay unavailable", http.StatusServiceUnavailable)
		case errors.Is(err, errLocalAdmissionForbidden):
			http.Error(w, "local Node operator required", http.StatusForbidden)
		default:
			http.Error(w, "publication failed", http.StatusConflict)
		}
		return
	}
	w.Header().Set("ETag", `"`+value.Revision+`"`)
	writeWorkspaceJSON(w, http.StatusOK, value)
}

func (h PublicationHandler) commands() publicationapp.Service {
	return publicationapp.Service{Store: h.Store, Exists: h.Exists, Ready: h.Ready, StopIfInactive: h.StopIfInactive, RelayURL: h.RelayURL}
}

func (h PublicationHandler) read(w http.ResponseWriter, r *http.Request, slug routing.WorkspaceSlug) {
	value, err := h.commands().Read(r.Context(), slug, func(context.Context) error { return permissionTransactionAdmission(r)() })
	if err != nil {
		http.Error(w, "publication unavailable", http.StatusConflict)
		return
	}
	w.Header().Set("ETag", `"`+value.Revision+`"`)
	writeWorkspaceJSON(w, http.StatusOK, value)
}

func (h PublicationHandler) relayURL() string {
	if h.RelayURL == nil {
		return ""
	}
	return h.RelayURL()
}
