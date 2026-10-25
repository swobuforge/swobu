package httpapi

import (
	"net/http"
	"net/url"

	connectapp "github.com/swobuforge/swobu/internal/app/operator/connect"
	"github.com/swobuforge/swobu/internal/clientconnect"
	"github.com/swobuforge/swobu/internal/routing"
)

// NewClientsHandler projects presence-only discovery through the existing
// Connect owner. It never plans, executes a client command or reads config bodies.
func NewClientsHandler(service *connectapp.Service, baseURL string, exists func(*http.Request, routing.WorkspaceSlug) bool) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !IsLocalOperatorRequest(request) {
			writeOperatorError(writer, http.StatusForbidden, "INVALID_ARGUMENT", "Local Node admission is required.")
			return
		}
		if request.Method != http.MethodGet {
			writer.Header().Set("Allow", http.MethodGet)
			writeOperatorError(writer, http.StatusMethodNotAllowed, "INVALID_ARGUMENT", "Client discovery method is not allowed.")
			return
		}
		query, err := url.ParseQuery(request.URL.RawQuery)
		if err != nil || len(query) != 1 || len(query["workspace"]) != 1 {
			writeOperatorError(writer, http.StatusBadRequest, "INVALID_ARGUMENT", "An exact Workspace is required.")
			return
		}
		slug, err := routing.ParseWorkspaceSlug(query.Get("workspace"))
		if err != nil || slug.String() != query.Get("workspace") {
			writeOperatorError(writer, http.StatusBadRequest, "INVALID_ARGUMENT", "An exact Workspace is required.")
			return
		}
		if exists == nil || !exists(request, slug) {
			writeOperatorError(writer, http.StatusNotFound, "NOT_FOUND", "Workspace is unavailable.")
			return
		}
		target, err := clientconnect.NewTarget(slug.String(), baseURL+"/workspaces/"+slug.String())
		if err != nil || service == nil {
			writeOperatorError(writer, http.StatusServiceUnavailable, "UNAVAILABLE", "Client discovery is unavailable.")
			return
		}
		clients, err := service.Discover(request.Context(), target)
		if err != nil || request.Context().Err() != nil {
			writeOperatorError(writer, http.StatusServiceUnavailable, "UNAVAILABLE", "Client discovery is unavailable.")
			return
		}
		writer.Header().Set("Cache-Control", "no-store")
		writeOperatorJSON(writer, http.StatusOK, clients)
	})
}
