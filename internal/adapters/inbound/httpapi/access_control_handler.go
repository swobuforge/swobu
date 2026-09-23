package httpapi

import (
	"errors"
	"net/http"

	"github.com/swobuforge/swobu/internal/accessstate"
	accessapp "github.com/swobuforge/swobu/internal/app/operator/access"
	"github.com/swobuforge/swobu/internal/routing"
)

type AccessControlHandler struct {
	service accessapp.Service
	mux     *http.ServeMux
}

func NewAccessControlHandler(service accessapp.Service) AccessControlHandler {
	h := AccessControlHandler{service: service, mux: http.NewServeMux()}
	h.mux.HandleFunc("GET /_swobu/access", h.list)
	h.mux.HandleFunc("POST /_swobu/access", h.create)
	h.mux.HandleFunc("POST /_swobu/access/{access}/credentials", h.issue)
	h.mux.HandleFunc("DELETE /_swobu/access/{access}/credentials/{credential}", h.revokeCredential)
	h.mux.HandleFunc("POST /_swobu/access/{access}/revoke", h.revoke)
	return h
}
func (h AccessControlHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}
func (h AccessControlHandler) list(w http.ResponseWriter, r *http.Request) {
	writeWorkspaceJSON(w, http.StatusOK, h.service.List(r.Context()))
}
func (h AccessControlHandler) create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name      string `json:"name"`
		Workspace string `json:"workspace"`
	}
	if !decodeAccessBody(w, r, &body) {
		return
	}
	result, err := h.service.Create(r.Context(), body.Name, body.Workspace)
	if err != nil {
		writeAccessError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeWorkspaceJSON(w, http.StatusCreated, result)
}
func (h AccessControlHandler) issue(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Workspace string `json:"workspace"`
	}
	if !decodeAccessBody(w, r, &body) {
		return
	}
	result, err := h.service.IssueCredential(r.Context(), r.PathValue("access"), body.Workspace)
	if err != nil {
		writeAccessError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeWorkspaceJSON(w, http.StatusCreated, result)
}
func (h AccessControlHandler) revokeCredential(w http.ResponseWriter, r *http.Request) {
	if !decodeAccessBody(w, r, &struct{}{}) {
		return
	}
	if err := h.service.RevokeCredential(r.Context(), r.PathValue("access"), r.PathValue("credential")); err != nil {
		writeAccessError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (h AccessControlHandler) revoke(w http.ResponseWriter, r *http.Request) {
	if !decodeAccessBody(w, r, &struct{}{}) {
		return
	}
	if err := h.service.Revoke(r.Context(), r.PathValue("access")); err != nil {
		writeAccessError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func decodeAccessBody(w http.ResponseWriter, r *http.Request, target any) bool {
	if err := decodeOperatorJSONObject(w, r, target, "access command"); err != nil {
		writeWorkspaceJSON(w, operatorJSONErrorStatus(err), map[string]string{"error": err.Error()})
		return false
	}
	return true
}
func writeAccessError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	if errors.Is(err, routing.ErrNotFound) || errors.Is(err, accessstate.ErrNotFound) {
		status = http.StatusNotFound
	}
	if errors.Is(err, accessstate.ErrRevoked) {
		status = http.StatusConflict
	}
	writeWorkspaceJSON(w, status, map[string]string{"error": err.Error()})
}
