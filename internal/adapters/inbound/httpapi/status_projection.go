package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	evidencestore "github.com/swobuforge/swobu/internal/adapters/outbound/evidence"
)

type statusProjectionReadFunc func(context.Context, evidencestore.ProjectionScope) (evidencestore.StatusProjection, error)

// StatusProjectionHandler renders the daemon-owned recent-traffic and counter
// projection for operator surfaces. It stays on the internal `_swobu/*` family
// so machine status and client protocol routes remain separate.
type StatusProjectionHandler struct {
	read statusProjectionReadFunc
}

func NewStatusProjectionHandler(read statusProjectionReadFunc) StatusProjectionHandler {
	return StatusProjectionHandler{read: read}
}

func (h StatusProjectionHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if h.read == nil {
		http.Error(w, "status projection unavailable", http.StatusInternalServerError)
		return
	}
	scope, err := parseProjectionScope(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	projection, err := h.read(req.Context(), scope)
	if err != nil {
		http.Error(w, "status projection unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(projection)
}

func parseProjectionScope(req *http.Request) (evidencestore.ProjectionScope, error) {
	raw := strings.TrimSpace(req.URL.Query().Get("scope")) // swobu:io-string source=boundary
	if raw == "" {
		return evidencestore.ProjectionScope{}, fmt.Errorf("status projection scope is required")
	}
	if raw == string(evidencestore.ProjectionScopeAll) {
		return evidencestore.ProjectionScope{Kind: evidencestore.ProjectionScopeAll}, nil
	}
	const endpointPrefix = "endpoint:"
	if strings.HasPrefix(raw, endpointPrefix) {
		endpoint := strings.TrimSpace(strings.TrimPrefix(raw, endpointPrefix)) // swobu:io-string source=boundary
		if endpoint == "" {
			return evidencestore.ProjectionScope{}, fmt.Errorf("status projection endpoint scope requires endpoint name")
		}
		return evidencestore.ProjectionScope{
			Kind:     evidencestore.ProjectionScopeEndpoint,
			Endpoint: endpoint,
		}, nil
	}
	return evidencestore.ProjectionScope{}, fmt.Errorf("status projection scope %q is invalid", raw)
}
