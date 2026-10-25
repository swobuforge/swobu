package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/swobuforge/swobu/internal/app/operator/workspaces"
	"github.com/swobuforge/swobu/internal/configstore"
	"github.com/swobuforge/swobu/internal/routing"
)

func (h workspaceControlHandlers) getServiceLimits(w http.ResponseWriter, r *http.Request) {
	if !IsLocalOperatorRequest(r) {
		http.Error(w, "local operator required", http.StatusForbidden)
		return
	}
	current, err := h.service.GetWorkspace(r.Context(), r.PathValue("workspace"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("ETag", `"`+current.ManagementETag+`"`)
	writeWorkspaceJSON(w, http.StatusOK, current.ServiceLimits)
}

func (h workspaceControlHandlers) setServiceLimits(w http.ResponseWriter, r *http.Request) {
	if !IsLocalOperatorRequest(r) {
		http.Error(w, "local operator required", http.StatusForbidden)
		return
	}
	etag := r.Header.Get("If-Match")
	if etag == "" {
		http.Error(w, "Workspace ETag required", http.StatusPreconditionRequired)
		return
	}
	if len(r.Header.Values("If-Match")) != 1 || len(etag) < 3 || etag[0] != '"' || etag[len(etag)-1] != '"' || strings.ContainsAny(etag[1:len(etag)-1], "\", \t\r\n") {
		http.Error(w, "one strong Workspace ETag required", http.StatusBadRequest)
		return
	}
	var policy routing.ServiceLimits
	if !decodeWorkspaceJSON(w, r, &policy) {
		return
	}
	result, err := h.service.SetServiceLimitsChecked(r.Context(), r.PathValue("workspace"), policy, etag[1:len(etag)-1], func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !IsLocalOperatorRequest(r.WithContext(ctx)) {
			return errLocalAdmissionForbidden
		}
		return nil
	})
	switch {
	case errors.Is(err, errLocalAdmissionForbidden):
		http.Error(w, "local authority revoked", http.StatusForbidden)
	case errors.Is(err, configstore.ErrStaleRevision):
		writeWorkspaceJSON(w, http.StatusPreconditionFailed, workspaces.CommandError{Code: workspaces.Conflict, Message: "Workspace ETag changed; refresh before applying"})
	case err != nil:
		h.writeError(w, err)
	default:
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("ETag", `"`+result.ManagementETag+`"`)
		writeWorkspaceJSON(w, http.StatusOK, result.ServiceLimits)
	}
}
