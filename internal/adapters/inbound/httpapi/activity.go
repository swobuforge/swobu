package httpapi

import (
	"net/http"
	"strings"

	executionevidence "github.com/swobuforge/swobu/internal/adapters/outbound/executionevidence"
	"github.com/swobuforge/swobu/internal/routing"
)

type activityReadFunc func(*http.Request, executionevidence.ProjectionScope) (executionevidence.ActivityProjection, error)

type ActivityHandler struct{ read activityReadFunc }

func NewActivityHandler(read activityReadFunc) ActivityHandler { return ActivityHandler{read: read} }

func (h ActivityHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeOperatorError(w, http.StatusMethodNotAllowed, "INVALID_ARGUMENT", "Activity method is not allowed.")
		return
	}
	if h.read == nil {
		writeOperatorError(w, http.StatusInternalServerError, "INTERNAL", "Activity is unavailable.")
		return
	}
	scope, err := parseActivityScope(req)
	if err != nil {
		writeOperatorError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "Activity scope is invalid.")
		return
	}
	activity, err := h.read(req, scope)
	if err != nil {
		writeOperatorError(w, http.StatusInternalServerError, "INTERNAL", "Activity is unavailable.")
		return
	}
	writeOperatorJSON(w, http.StatusOK, activity)
}

func parseActivityScope(req *http.Request) (executionevidence.ProjectionScope, error) {
	workspace := strings.TrimSpace(req.URL.Query().Get("workspace"))
	if workspace == "" {
		return executionevidence.ProjectionScope{Kind: executionevidence.ProjectionScopeAll}, nil
	}
	slug, err := routing.ParseWorkspaceSlug(workspace)
	if err != nil {
		return executionevidence.ProjectionScope{}, err
	}
	return executionevidence.ProjectionScope{Kind: executionevidence.ProjectionScopeWorkspace, Workspace: slug.String()}, nil
}
