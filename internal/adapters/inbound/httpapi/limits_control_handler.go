package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/swobuforge/swobu/internal/accounting"
	"github.com/swobuforge/swobu/internal/app/operator/adminapproval"
	limitapp "github.com/swobuforge/swobu/internal/app/operator/limits"
	"github.com/swobuforge/swobu/internal/routing"
)

type LimitsControlHandler struct {
	store           *accounting.Store
	workspaceExists func(string) bool
	approvals       *adminapproval.Service
}

func NewLimitsControlHandler(store *accounting.Store, workspaceExists func(string) bool) LimitsControlHandler {
	return LimitsControlHandler{store: store, workspaceExists: workspaceExists}
}

// WithApprovals attaches the trusted local approval authority. The handler
// remains fail-closed when it is absent; ordinary Workspace traffic cannot
// reach this Node Control route.
func (h LimitsControlHandler) WithApprovals(service *adminapproval.Service) LimitsControlHandler {
	h.approvals = service
	return h
}

func (h LimitsControlHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Even an empty successful clear response belongs to the secret-free,
	// noncacheable Operator control transport.
	w.Header().Set("Cache-Control", "no-store")
	rawWorkspace := r.PathValue("workspace")
	if rawWorkspace == "" {
		rawWorkspace = strings.TrimPrefix(r.URL.Path, "/api/v1/workspaces/")
		rawWorkspace = strings.SplitN(rawWorkspace, "/", 2)[0]
	}
	workspace, err := routing.ParseWorkspaceSlug(rawWorkspace)
	if err != nil || h.workspaceExists == nil || !h.workspaceExists(workspace.String()) {
		writeWorkspaceJSON(w, http.StatusNotFound, map[string]string{"error": "workspace not found"})
		return
	}
	if r.Method == http.MethodGet {
		if strings.HasSuffix(r.URL.Path, "/limits/status") {
			if !IsLocalOperatorRequest(r) {
				http.Error(w, "trusted local operator context required", http.StatusForbidden)
				return
			}
			status, revision := h.store.StatusWithRevision(workspace.String())
			w.Header().Set("ETag", `"`+revision+`"`)
			writeWorkspaceJSON(w, http.StatusOK, status)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/limits/changes") {
			if !IsLocalOperatorRequest(r) {
				http.Error(w, "trusted local operator context required", http.StatusForbidden)
				return
			}
			writeWorkspaceJSON(w, http.StatusOK, h.store.LimitChanges(workspace.String()))
			return
		}
		limit, ok := h.store.Limit(workspace.String())
		if !ok {
			writeWorkspaceJSON(w, http.StatusNotFound, map[string]string{"error": "limit not configured"})
			return
		}
		w.Header().Set("ETag", `"`+h.store.Revision()+`"`)
		writeWorkspaceJSON(w, http.StatusOK, limit)
		return
	}
	if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/preview") {
		h.preview(w, r, workspace.String())
		return
	}
	if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/apply") {
		h.apply(w, r, workspace.String())
		return
	}
	if r.Method != http.MethodPut && r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if r.Method == http.MethodDelete && r.Header.Get("If-Match") == "" {
		writeWorkspaceJSON(w, http.StatusPreconditionRequired, map[string]string{"error": "If-Match is required to clear a limit"})
		return
	}
	if match := r.Header.Get("If-Match"); match != "" && match != `"`+h.store.Revision()+`"` {
		status := http.StatusConflict
		if r.Method == http.MethodDelete {
			status = http.StatusPreconditionFailed
		}
		writeWorkspaceJSON(w, status, map[string]string{"error": "limit revision conflict"})
		return
	}
	if r.Method == http.MethodDelete {
		if err := limitapp.New(h.store, h.approvals, h.workspaceExists).Clear(r.Context(), workspace.String(), strings.Trim(r.Header.Get("If-Match"), `"`), func(context.Context) error { return permissionTransactionAdmission(r)() }); err != nil {
			writeWorkspaceJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	var limit accounting.Limit
	if err := decodeOperatorJSONObject(w, r, &limit, "workspace limit"); err != nil {
		writeWorkspaceJSON(w, operatorJSONErrorStatus(err), map[string]string{"error": err.Error()})
		return
	}
	if err := h.store.SetLimit(workspace.String(), limit); err != nil {
		writeWorkspaceJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("ETag", `"`+h.store.Revision()+`"`)
	w.Header().Set("Location", "/api/v1/workspaces/"+workspace.String()+"/limits")
	writeWorkspaceJSON(w, http.StatusOK, limit)
}

func (h LimitsControlHandler) preview(w http.ResponseWriter, r *http.Request, workspace string) {
	if h.approvals == nil {
		writeWorkspaceJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "admin approvals unavailable"})
		return
	}
	var limit accounting.Limit
	if err := decodeOperatorJSONObject(w, r, &limit, "workspace limit preview"); err != nil {
		writeWorkspaceJSON(w, operatorJSONErrorStatus(err), map[string]string{"error": err.Error()})
		return
	}
	approval, err := limitapp.New(h.store, h.approvals, h.workspaceExists).Preview(r.Context(), workspace, limit, func(context.Context) error { return permissionTransactionAdmission(r)() })
	if err != nil {
		writeWorkspaceJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeWorkspaceJSON(w, http.StatusOK, approval)
}

func (h LimitsControlHandler) apply(w http.ResponseWriter, r *http.Request, workspace string) {
	if h.approvals == nil {
		writeWorkspaceJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "admin approvals unavailable"})
		return
	}
	var body struct {
		ApprovalID string           `json:"approval_id"`
		Limit      accounting.Limit `json:"limit"`
	}
	if err := decodeOperatorJSONObject(w, r, &body, "workspace limit apply"); err != nil {
		writeWorkspaceJSON(w, operatorJSONErrorStatus(err), map[string]string{"error": err.Error()})
		return
	}
	if err := limitapp.New(h.store, h.approvals, h.workspaceExists).Apply(r.Context(), workspace, body.ApprovalID, body.Limit, func(context.Context) error { return permissionTransactionAdmission(r)() }); err != nil {
		writeWorkspaceJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("ETag", `"`+h.store.Revision()+`"`)
	writeWorkspaceJSON(w, http.StatusOK, body.Limit)
}
