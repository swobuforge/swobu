package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/swobuforge/swobu/internal/admission"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/routing"
)

func admitOperationalHTTP(w http.ResponseWriter, r *http.Request) (func(), bool) {
	noop := func() {}
	if r.Method != http.MethodPost && !publicWorkspaceSearchRequest(r) {
		return noop, true
	}
	service := requestedWorkspaceService(r.URL.Path)
	// MCP is charged only at tools/call, not initialize or tools/list.
	if service == "" || service == routing.ServiceMCP {
		return noop, true
	}
	workspace, err := meteredWorkspace(r)
	if err != nil {
		http.NotFound(w, r)
		return noop, false
	}
	release, retry, err := AdmitWorkspaceOperationalOperation(r.Context(), workspace, service)
	if err == nil {
		return release, true
	}
	w.Header().Set("Cache-Control", "no-store")
	if errors.Is(err, routing.ErrNotFound) {
		writeSwobuError(w, canonical.BadEndpoint("Workspace "+workspace.String()+" does not exist."))
		return noop, false
	}
	if errors.Is(err, admission.ErrExhausted) {
		seconds := int((retry + time.Second - 1) / time.Second)
		if seconds < 1 {
			seconds = 1
		}
		w.Header().Set("Retry-After", strconv.Itoa(seconds))
		http.Error(w, "Workspace Service operational limit exceeded", http.StatusTooManyRequests)
	} else {
		http.Error(w, "Workspace operational admission unavailable", http.StatusServiceUnavailable)
	}
	return noop, false
}
