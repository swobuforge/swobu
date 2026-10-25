package httpapi

import (
	"errors"
	"net/http"
	"net/url"

	"github.com/swobuforge/swobu/internal/app/operator/managementaudit"
)

func NewAuditHandler(service managementaudit.Service) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Cache-Control", "no-store")
		if !IsLocalOperatorRequest(request) {
			http.Error(writer, "trusted local operator context required", http.StatusForbidden)
			return
		}
		if request.URL.Path != "/api/v1/audit/events" {
			http.NotFound(writer, request)
			return
		}
		if request.Method != http.MethodGet {
			writer.Header().Set("Allow", http.MethodGet)
			writer.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		query, err := parseAuditQuery(request)
		if err != nil {
			http.Error(writer, "audit requires one exact Workspace query", http.StatusBadRequest)
			return
		}
		result, err := service.Read(query)
		if err != nil {
			http.Error(writer, "audit requires one exact Workspace query", http.StatusBadRequest)
			return
		}
		if !IsLocalOperatorRequest(request) {
			http.Error(writer, "trusted local operator context required", http.StatusForbidden)
			return
		}
		writeWorkspaceJSON(writer, http.StatusOK, result)
	})
}

func parseAuditQuery(request *http.Request) (string, error) {
	query, err := url.ParseQuery(request.URL.RawQuery)
	if err == nil && len(query) == 0 && request.URL.RawQuery == "" {
		return "", nil
	}
	if err != nil || len(query) != 1 || len(query["workspace"]) != 1 || query.Get("workspace") == "" {
		return "", errors.New("invalid audit scope")
	}
	return query.Get("workspace"), nil
}
