package httpapi

import (
	"net/http"

	"github.com/swobuforge/swobu/internal/app/operator/shares"
	"github.com/swobuforge/swobu/internal/sharestate"
)

type ShareControlHandler struct{ service shares.Service }

func NewShareControlHandler(service shares.Service) ShareControlHandler {
	return ShareControlHandler{service: service}
}

func (h ShareControlHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		if routeRef := r.URL.Query().Get("route"); routeRef != "" {
			result, err := h.service.Reveal(routeRef)
			if err != nil {
				writeWorkspaceJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
				return
			}
			writeWorkspaceJSON(w, http.StatusOK, result)
			return
		}
		summaries, err := h.service.List()
		if err != nil {
			writeWorkspaceJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeWorkspaceJSON(w, http.StatusOK, summaries)
		return
	}
	var body struct {
		Route   string            `json:"route"`
		Expires sharestate.Expiry `json:"expires"`
	}
	if err := decodeOperatorJSONObject(w, r, &body, "share command"); err != nil {
		writeWorkspaceJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	switch r.Method {
	case http.MethodPost:
		result, err := h.service.Issue(r.Context(), body.Route, body.Expires)
		if err != nil {
			writeWorkspaceJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeWorkspaceJSON(w, http.StatusOK, result)
	case http.MethodDelete:
		if err := h.service.Revoke(body.Route); err != nil {
			writeWorkspaceJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}
