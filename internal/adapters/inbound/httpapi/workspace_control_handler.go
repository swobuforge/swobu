package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/swobuforge/swobu/internal/app/operator/workspaces"
)

type workspaceControlHandlers struct{ service workspaces.Service }

// NewWorkspaceControlHandler registers the semantic workspace command surface
// with method-aware standard-library patterns.
func NewWorkspaceControlHandler(service workspaces.Service) *http.ServeMux {
	h := workspaceControlHandlers{service: service}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /_swobu/workspaces", h.listWorkspaces)
	mux.HandleFunc("POST /_swobu/workspaces", h.createWorkspace)
	mux.HandleFunc("GET /_swobu/workspaces/{workspace}", h.getWorkspace)
	mux.HandleFunc("DELETE /_swobu/workspaces/{workspace}", h.deleteWorkspace)
	mux.HandleFunc("POST /_swobu/workspaces/{workspace}/rename", h.renameWorkspace)
	mux.HandleFunc("POST /_swobu/workspaces/{workspace}/default-route", h.setDefaultRoute)
	mux.HandleFunc("POST /_swobu/workspaces/{workspace}/routes", h.createRoute)
	mux.HandleFunc("DELETE /_swobu/workspaces/{workspace}/routes/{route}", h.deleteRoute)
	mux.HandleFunc("GET /_swobu/workspaces/{workspace}/routes/{route}", h.getRoute)
	mux.HandleFunc("PUT /_swobu/workspaces/{workspace}/routes/{route}", h.replaceRoute)
	mux.HandleFunc("POST /_swobu/workspaces/{workspace}/routes/{route}/rename", h.renameRoute)
	return mux
}

func (h workspaceControlHandlers) listWorkspaces(w http.ResponseWriter, r *http.Request) {
	value, err := h.service.ListWorkspaces(r.Context())
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeWorkspaceJSON(w, http.StatusOK, value)
}

func (h workspaceControlHandlers) createWorkspace(w http.ResponseWriter, r *http.Request) {
	var cmd workspaces.CreateWorkspace
	if !decodeWorkspaceJSON(w, r, &cmd) {
		return
	}
	value, err := h.service.CreateWorkspace(r.Context(), cmd)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeWorkspaceJSON(w, http.StatusCreated, value)
}

func (h workspaceControlHandlers) getWorkspace(w http.ResponseWriter, r *http.Request) {
	value, err := h.service.GetWorkspace(r.Context(), r.PathValue("workspace"))
	h.writeWorkspace(w, value, err)
}

func (h workspaceControlHandlers) deleteWorkspace(w http.ResponseWriter, r *http.Request) {
	if err := h.service.DeleteWorkspace(r.Context(), r.PathValue("workspace")); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h workspaceControlHandlers) renameWorkspace(w http.ResponseWriter, r *http.Request) {
	var body struct {
		NewSlug string `json:"new_slug"`
	}
	if !decodeWorkspaceJSON(w, r, &body) {
		return
	}
	value, err := h.service.RenameWorkspace(r.Context(), workspaces.RenameWorkspace{Slug: r.PathValue("workspace"), NewSlug: body.NewSlug})
	h.writeWorkspace(w, value, err)
}

func (h workspaceControlHandlers) setDefaultRoute(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Route string `json:"route"`
	}
	if !decodeWorkspaceJSON(w, r, &body) {
		return
	}
	value, err := h.service.SetDefaultRoute(r.Context(), workspaces.SetDefaultRoute{Workspace: r.PathValue("workspace"), Route: body.Route})
	h.writeWorkspace(w, value, err)
}

func (h workspaceControlHandlers) createRoute(w http.ResponseWriter, r *http.Request) {
	var cmd workspaces.CreateRoute
	if !decodeWorkspaceJSON(w, r, &cmd) {
		return
	}
	cmd.Workspace = r.PathValue("workspace")
	value, err := h.service.CreateRoute(r.Context(), cmd)
	h.writeWorkspace(w, value, err)
}

func (h workspaceControlHandlers) deleteRoute(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Replacement string `json:"replacement,omitempty"`
	}
	if r.Body != nil && r.ContentLength != 0 && !decodeWorkspaceJSON(w, r, &body) {
		return
	}
	value, err := h.service.DeleteRoute(r.Context(), workspaces.DeleteRoute{
		Workspace: r.PathValue("workspace"), Route: r.PathValue("route"), Replacement: body.Replacement,
	})
	h.writeWorkspace(w, value, err)
}

func (h workspaceControlHandlers) getRoute(w http.ResponseWriter, r *http.Request) {
	value, err := h.service.GetRoute(r.Context(), r.PathValue("workspace"), r.PathValue("route"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeWorkspaceJSON(w, http.StatusOK, value)
}

func (h workspaceControlHandlers) replaceRoute(w http.ResponseWriter, r *http.Request) {
	var spec workspaces.RouteSpec
	if !decodeWorkspaceJSON(w, r, &spec) {
		return
	}
	value, err := h.service.ReplaceRoute(r.Context(), workspaces.ReplaceRoute{
		Workspace: r.PathValue("workspace"), Route: r.PathValue("route"), Spec: spec,
	})
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeWorkspaceJSON(w, http.StatusOK, value)
}

func (h workspaceControlHandlers) renameRoute(w http.ResponseWriter, r *http.Request) {
	var body struct {
		NewName string `json:"new_name"`
	}
	if !decodeWorkspaceJSON(w, r, &body) {
		return
	}
	value, err := h.service.RenameRoute(r.Context(), workspaces.RenameRoute{
		Workspace: r.PathValue("workspace"), Route: r.PathValue("route"), NewName: body.NewName,
	})
	h.writeWorkspace(w, value, err)
}

func decodeWorkspaceJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	if err := decodeOperatorJSONObject(w, r, target, "workspace command"); err != nil {
		writeWorkspaceJSON(w, http.StatusBadRequest, workspaces.CommandError{Code: workspaces.InvalidArgument, Message: "invalid command body: " + err.Error()})
		return false
	}
	return true
}

func (h workspaceControlHandlers) writeWorkspace(w http.ResponseWriter, value workspaces.Workspace, err error) {
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeWorkspaceJSON(w, http.StatusOK, value)
}

func (h workspaceControlHandlers) writeError(w http.ResponseWriter, err error) {
	var command workspaces.CommandError
	if !errors.As(err, &command) {
		command = workspaces.CommandError{Code: workspaces.Internal, Message: err.Error()}
	}
	status := http.StatusInternalServerError
	switch command.Code {
	case workspaces.InvalidArgument:
		status = http.StatusBadRequest
	case workspaces.NotFound:
		status = http.StatusNotFound
	case workspaces.Conflict:
		status = http.StatusConflict
	case workspaces.Unavailable:
		status = http.StatusServiceUnavailable
	}
	writeWorkspaceJSON(w, status, command)
}

func writeWorkspaceJSON(w http.ResponseWriter, status int, value any) {
	raw, err := json.Marshal(value)
	if err != nil {
		http.Error(w, fmt.Sprintf("encode response: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(append(raw, '\n'))
}
