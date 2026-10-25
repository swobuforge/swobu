package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/swobuforge/swobu/internal/app/operator/workspaces"
	"github.com/swobuforge/swobu/internal/configstore"
	"github.com/swobuforge/swobu/internal/routing"
)

type workspaceControlHandlers struct{ service workspaces.Service }

// NewNodeControlAPIHandler is the sole Workspace management HTTP contract.
// Runtime Workspace data paths are built by independent handlers.
func NewNodeControlAPIHandler(service workspaces.Service) *http.ServeMux {
	h := workspaceControlHandlers{service: service}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/workspaces", h.listWorkspaces)
	mux.HandleFunc("POST /api/v1/workspaces", h.createWorkspaceChecked)
	mux.HandleFunc("GET /api/v1/workspaces/{workspace}", h.getWorkspace)
	mux.HandleFunc("DELETE /api/v1/workspaces/{workspace}", h.deleteWorkspaceChecked)
	mux.HandleFunc("PATCH /api/v1/workspaces/{workspace}", h.updateWorkspaceDisplayName)
	mux.HandleFunc("GET /api/v1/workspaces/{workspace}/services", h.listServices)
	mux.HandleFunc("GET /api/v1/workspaces/{workspace}/service-limits", h.getServiceLimits)
	mux.HandleFunc("PUT /api/v1/workspaces/{workspace}/service-limits", h.setServiceLimits)
	mux.HandleFunc("PUT /api/v1/workspaces/{workspace}/services/{service}", h.enableService)
	mux.HandleFunc("DELETE /api/v1/workspaces/{workspace}/services/{service}", h.disableService)
	mux.HandleFunc("GET /api/v1/workspaces/{workspace}/model-routes", h.listModelRoutes)
	mux.HandleFunc("PATCH /api/v1/workspaces/{workspace}/model-routes", h.reorderModelRoutes)
	mux.HandleFunc("GET /api/v1/workspaces/{workspace}/model-match", h.previewModelMatch)
	mux.HandleFunc("GET /api/v1/workspaces/{workspace}/model-routes/{route}", h.getRoute)
	mux.HandleFunc("PUT /api/v1/workspaces/{workspace}/model-routes/{route}", h.replaceRoute)
	mux.HandleFunc("DELETE /api/v1/workspaces/{workspace}/model-routes/{route}", h.deleteModelRouteChecked)
	mux.HandleFunc("GET /api/v1/workspaces/{workspace}/mcp-mounts", h.listMCPMounts)
	mux.HandleFunc("GET /api/v1/workspaces/{workspace}/mcp-mounts/{alias}", h.getMCPMount)
	mux.HandleFunc("PUT /api/v1/workspaces/{workspace}/mcp-mounts/{alias}", h.setMCPMount)
	mux.HandleFunc("DELETE /api/v1/workspaces/{workspace}/mcp-mounts/{alias}", h.deleteMCPMountChecked)
	mux.HandleFunc("GET /api/v1/workspaces/{workspace}/search-route", h.getSearchRoute)
	mux.HandleFunc("PUT /api/v1/workspaces/{workspace}/search-route", h.setSearchRoute)
	mux.HandleFunc("DELETE /api/v1/workspaces/{workspace}/search-route", h.deleteSearchRoute)
	return mux
}

func (h workspaceControlHandlers) listMCPMounts(w http.ResponseWriter, r *http.Request) {
	value, err := h.service.GetWorkspace(r.Context(), r.PathValue("workspace"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	type mount struct {
		Alias         string   `json:"alias"`
		ConnectionID  string   `json:"connection_id"`
		Builtin       string   `json:"builtin,omitempty"`
		SelectedTools []string `json:"selected_tools"`
	}
	out := make([]mount, 0, len(value.MCPMounts))
	for _, item := range value.MCPMounts {
		out = append(out, mount{Alias: item.Alias, Builtin: item.Builtin, ConnectionID: item.ConnectionID, SelectedTools: append([]string{}, item.AllowedTools...)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Alias < out[j].Alias })
	writeWorkspaceJSON(w, http.StatusOK, out)
}

func (h workspaceControlHandlers) getMCPMount(w http.ResponseWriter, r *http.Request) {
	value, err := h.service.GetWorkspace(r.Context(), r.PathValue("workspace"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	for _, item := range value.MCPMounts {
		if item.Alias == r.PathValue("alias") {
			writeWorkspaceJSON(w, http.StatusOK, struct {
				Alias         string   `json:"alias"`
				ConnectionID  string   `json:"connection_id"`
				Builtin       string   `json:"builtin,omitempty"`
				SelectedTools []string `json:"selected_tools"`
			}{item.Alias, item.ConnectionID, item.Builtin, append([]string{}, item.AllowedTools...)})
			return
		}
	}
	h.writeError(w, workspaces.CommandError{Code: workspaces.NotFound, Message: "MCP mount not found"})
}

// updateWorkspaceDisplayName accepts only presentation metadata. Unknown
// additive fields follow the Operator envelope law, but identity-shaped known
// fields explicitly fail rather than making a slug migration look successful.
func (h workspaceControlHandlers) updateWorkspaceDisplayName(w http.ResponseWriter, r *http.Request) {
	if !IsLocalOperatorRequest(r) {
		http.Error(w, "trusted local operator context required", http.StatusForbidden)
		return
	}
	if r.URL.RawQuery != "" {
		http.Error(w, "Workspace metadata update does not accept query parameters", http.StatusBadRequest)
		return
	}
	values := r.Header.Values("If-Match")
	if len(values) == 0 || (len(values) == 1 && values[0] == "") {
		http.Error(w, "Workspace revision required", http.StatusPreconditionRequired)
		return
	}
	etag := values[0]
	if len(values) != 1 || len(etag) < 3 || etag[0] != '"' || etag[len(etag)-1] != '"' || strings.ContainsAny(etag[1:len(etag)-1], "\"\\, \t\r\n") {
		http.Error(w, "one strong Workspace ETag required", http.StatusBadRequest)
		return
	}
	var fields map[string]json.RawMessage
	if !decodeWorkspaceJSON(w, r, &fields) {
		return
	}
	for _, name := range []string{"slug", "new_slug"} {
		if _, exists := fields[name]; exists {
			writeWorkspaceJSON(w, http.StatusBadRequest, workspaces.CommandError{Code: workspaces.InvalidArgument, Message: "Workspace slug is immutable; PATCH changes display_name only"})
			return
		}
	}
	raw, exists := fields["display_name"]
	var name string
	if !exists || string(raw) == "null" || json.Unmarshal(raw, &name) != nil {
		writeWorkspaceJSON(w, http.StatusBadRequest, workspaces.CommandError{Code: workspaces.InvalidArgument, Message: "display_name must be an explicit string; empty clears it"})
		return
	}
	value, err := h.service.SetDisplayNameChecked(r.Context(), r.PathValue("workspace"), name, strings.Trim(r.Header.Get("If-Match"), `"`), func(ctx context.Context) error {
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
		writeWorkspaceJSON(w, http.StatusPreconditionFailed, workspaces.CommandError{Code: workspaces.Conflict, Message: "Workspace ETag changed; obtain fresh state before applying"})
	case err != nil:
		h.writeError(w, err)
	default:
		w.Header().Set("ETag", `"`+value.ManagementETag+`"`)
		w.Header().Set("Cache-Control", "no-store")
		writeWorkspaceJSON(w, http.StatusOK, value)
	}
}

func (h workspaceControlHandlers) getSearchRoute(w http.ResponseWriter, r *http.Request) {
	value, err := h.service.GetWorkspace(r.Context(), r.PathValue("workspace"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	connectionID := value.SearchConnectionID
	if connectionID == "" {
		h.writeError(w, workspaces.CommandError{Code: workspaces.NotFound, Message: "Search route is not configured"})
		return
	}
	writeWorkspaceJSON(w, http.StatusOK, struct {
		ConnectionID          string   `json:"connection_id"`
		FallbackConnectionIDs []string `json:"fallback_connection_ids"`
	}{connectionID, append([]string{}, value.SearchFallbackIDs...)})
}

func (h workspaceControlHandlers) setSearchRoute(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ConnectionID          string          `json:"connection_id"`
		FallbackConnectionIDs []string        `json:"fallback_connection_ids"`
		SourceID              json.RawMessage `json:"source_id"` // Reject, never interpret.
	}
	if !decodeWorkspaceJSON(w, r, &body) {
		return
	}
	if body.SourceID != nil {
		http.Error(w, "source_id is retired; use connection_id", http.StatusBadRequest)
		return
	}
	// SearchRoute requires an array on the wire; nil means missing or null.
	// Empty [] explicitly replaces the route with one primary Connection.
	if body.FallbackConnectionIDs == nil {
		http.Error(w, "fallback_connection_ids must be an array", http.StatusBadRequest)
		return
	}
	if body.ConnectionID == "" {
		http.Error(w, "connection_id is required", http.StatusBadRequest)
		return
	}
	h.changeSearchRouteChecked(w, r, body.ConnectionID, body.FallbackConnectionIDs)
}

func (h workspaceControlHandlers) deleteSearchRoute(w http.ResponseWriter, r *http.Request) {
	h.changeSearchRouteChecked(w, r, "", nil)
}

func (h workspaceControlHandlers) setMCPMount(w http.ResponseWriter, r *http.Request) {
	if !IsLocalOperatorRequest(r) {
		http.Error(w, "trusted local operator context required", http.StatusForbidden)
		return
	}
	dryRun, ok := permissionDryRun(w, r)
	query, queryErr := url.ParseQuery(r.URL.RawQuery)
	if !ok || queryErr != nil || len(query) != 1 {
		if ok {
			http.Error(w, "explicit dry_run=true or false required", http.StatusBadRequest)
		}
		return
	}
	var body struct {
		Alias         string                            `json:"alias"`
		ConnectionID  string                            `json:"connection_id"`
		Builtin       string                            `json:"builtin,omitempty"`
		SelectedTools []string                          `json:"selected_tools"`
		Review        *workspaces.MCPMountReviewRequest `json:"review,omitempty"`
		SourceID      json.RawMessage                   `json:"source_id"`
		Integration   json.RawMessage                   `json:"integration"`
		AllowTools    json.RawMessage                   `json:"allow_tools"`
	}
	if !decodeWorkspaceJSON(w, r, &body) {
		return
	}
	if body.SourceID != nil || body.Integration != nil || body.AllowTools != nil {
		http.Error(w, "retired source_id, integration and allow_tools fields are not accepted", http.StatusBadRequest)
		return
	}
	if body.SelectedTools == nil {
		http.Error(w, "selected_tools is required", http.StatusBadRequest)
		return
	}
	if body.ConnectionID == "" && body.Builtin == "" {
		http.Error(w, "connection_id is required", http.StatusBadRequest)
		return
	}
	alias := r.PathValue("alias")
	if body.Alias != "" && body.Alias != alias {
		http.Error(w, "mount alias must match the resource path", http.StatusBadRequest)
		return
	}
	command := workspaces.MCPMountDraft{Workspace: r.PathValue("workspace"), Alias: alias, Builtin: body.Builtin, ConnectionID: body.ConnectionID, SelectedTools: body.SelectedTools}
	if dryRun {
		if body.Review != nil {
			http.Error(w, "preview request must not contain an apply review", http.StatusBadRequest)
			return
		}
		preview, err := h.service.PreviewMCPMount(r.Context(), command)
		if err != nil {
			h.writeError(w, err)
			return
		}
		w.Header().Set("ETag", `"`+preview.Revision+`"`)
		w.Header().Set("Cache-Control", "no-store")
		writeWorkspaceJSON(w, http.StatusOK, preview)
		return
	}
	if body.Review == nil {
		http.Error(w, "reviewed MCP mount digest and owner revisions are required", http.StatusBadRequest)
		return
	}
	if !permissionRevisionPrecondition(w, r) {
		return
	}
	etag := strings.Trim(r.Header.Get("If-Match"), `"`)
	if body.Review.WorkspaceETag != etag {
		http.Error(w, "review Workspace ETag does not match If-Match", http.StatusBadRequest)
		return
	}
	value, err := h.service.ApplyMCPMountChecked(r.Context(), command, *body.Review, func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !IsLocalOperatorRequest(r.WithContext(ctx)) {
			return errLocalAdmissionForbidden
		}
		return nil
	})
	if errors.Is(err, errLocalAdmissionForbidden) {
		http.Error(w, "local authority revoked", http.StatusForbidden)
		return
	}
	if errors.Is(err, workspaces.ErrMCPMountReviewChanged) {
		writeWorkspaceJSON(w, http.StatusPreconditionFailed, workspaces.CommandError{Code: workspaces.Conflict, Message: "MCP mount review changed; obtain a fresh preview"})
		return
	}
	if err != nil {
		h.writeError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeWorkspaceJSON(w, http.StatusOK, value)
}

func (h workspaceControlHandlers) listWorkspaces(w http.ResponseWriter, r *http.Request) {
	value, err := h.service.ListWorkspaces(r.Context())
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeWorkspaceJSON(w, http.StatusOK, value)
}

func (h workspaceControlHandlers) createWorkspaceChecked(w http.ResponseWriter, r *http.Request) {
	if !IsLocalOperatorRequest(r) {
		http.Error(w, "trusted local operator context required", http.StatusForbidden)
		return
	}
	query, queryErr := url.ParseQuery(r.URL.RawQuery)
	if queryErr != nil || r.URL.ForceQuery {
		http.Error(w, "malformed creation query", http.StatusBadRequest)
		return
	}
	for name, values := range query {
		if name != "dry_run" || len(values) != 1 || (values[0] != "true" && values[0] != "false") {
			http.Error(w, "unsupported creation query", http.StatusBadRequest)
			return
		}
	}
	authorize := func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !IsLocalOperatorRequest(r.WithContext(ctx)) {
			return errLocalAdmissionForbidden
		}
		return nil
	}
	if query.Get("dry_run") == "true" {
		var cmd workspaces.CreateWorkspace
		if !decodeWorkspaceJSON(w, r, &cmd) {
			return
		}
		value, err := h.service.PreviewCreation(r.Context(), cmd, authorize)
		if errors.Is(err, errLocalAdmissionForbidden) {
			http.Error(w, "local authority revoked", http.StatusForbidden)
			return
		}
		if err != nil {
			h.writeError(w, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeWorkspaceJSON(w, http.StatusOK, value)
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if len(r.Header.Values("Idempotency-Key")) != 1 || len(key) == 0 || len(key) > 256 {
		http.Error(w, "one creation Idempotency-Key required", http.StatusBadRequest)
		return
	}
	for _, value := range key {
		if value < 33 || value > 126 {
			http.Error(w, "invalid creation Idempotency-Key", http.StatusBadRequest)
			return
		}
	}
	var cmd workspaces.CreateWorkspace
	if !decodeWorkspaceJSON(w, r, &cmd) {
		return
	}
	value, replayed, err := h.service.CreateWorkspaceChecked(r.Context(), cmd, "local:POST:workspace", key, authorize)
	switch {
	case errors.Is(err, errLocalAdmissionForbidden):
		http.Error(w, "local authority revoked", http.StatusForbidden)
		return
	case errors.Is(err, configstore.ErrIdempotencyConflict):
		writeWorkspaceJSON(w, http.StatusConflict, workspaces.CommandError{Code: workspaces.Conflict, Message: "creation key reused with different intent"})
		return
	case err != nil:
		h.writeError(w, err)
		return
	}
	w.Header().Set("Location", "/api/v1/workspaces/"+url.PathEscape(value.Slug))
	w.Header().Set("ETag", `"`+value.ManagementETag+`"`)
	w.Header().Set("Cache-Control", "no-store")
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	}
	writeWorkspaceJSON(w, status, value)
}

func (h workspaceControlHandlers) getWorkspace(w http.ResponseWriter, r *http.Request) {
	value, err := h.service.GetWorkspace(r.Context(), r.PathValue("workspace"))
	if err == nil {
		w.Header().Set("ETag", `"`+value.ManagementETag+`"`)
	}
	h.writeWorkspace(w, value, err)
}

// Workspace deletion has only the checked versioned command.
// Trusted confirmation belongs to the consumer; no model-facing tool mounts
// this handler. Admission is repeated inside the routing transaction on replay.
func (h workspaceControlHandlers) deleteWorkspaceChecked(w http.ResponseWriter, r *http.Request) {
	if !IsLocalOperatorRequest(r) {
		http.Error(w, "trusted local operator context required", http.StatusForbidden)
		return
	}
	query, queryErr := url.ParseQuery(r.URL.RawQuery)
	if queryErr != nil {
		writeWorkspaceJSON(w, 400, workspaces.CommandError{Code: workspaces.InvalidArgument, Message: "malformed deletion query"})
		return
	}
	for key, values := range query {
		if key != "dry_run" || len(values) != 1 || (values[0] != "true" && values[0] != "false") {
			writeWorkspaceJSON(w, 400, workspaces.CommandError{Code: workspaces.InvalidArgument, Message: "unsupported deletion query"})
			return
		}
	}
	if !decodeWorkspaceJSON(w, r, &struct{}{}) {
		return
	}
	if query.Get("dry_run") == "true" {
		value, err := h.service.PreviewDeletion(r.Context(), r.PathValue("workspace"))
		if err != nil {
			h.writeError(w, err)
			return
		}
		w.Header().Set("ETag", `"`+value.ManagementETag+`"`)
		writeWorkspaceJSON(w, 200, value)
		return
	}
	if len(r.Header.Values("If-Match")) == 0 || r.Header.Get("If-Match") == "" {
		writeWorkspaceJSON(w, 428, workspaces.CommandError{Code: workspaces.InvalidArgument, Message: "If-Match required"})
		return
	}
	etag := r.Header.Get("If-Match")
	if len(r.Header.Values("If-Match")) != 1 || len(etag) < 3 || etag[0] != '"' || etag[len(etag)-1] != '"' || strings.ContainsAny(etag[1:len(etag)-1], "\"\\, \t\r\n") {
		writeWorkspaceJSON(w, 400, workspaces.CommandError{Code: workspaces.InvalidArgument, Message: "one exact strong If-Match required"})
		return
	}
	key := r.Header.Get("Idempotency-Key")
	// The 256-byte envelope bounds untrusted header processing; the store keeps
	// only a digest. Empty/ambiguous keys cannot select a replay transaction.
	if len(r.Header.Values("Idempotency-Key")) != 1 || len(key) == 0 || len(key) > 256 || strings.ContainsAny(key, " \t\r\n") {
		writeWorkspaceJSON(w, 400, workspaces.CommandError{Code: workspaces.InvalidArgument, Message: "Idempotency-Key required (1-256 bytes, no whitespace)"})
		return
	}
	_, err := h.service.DeleteWorkspaceChecked(r.Context(), r.PathValue("workspace"), etag[1:len(etag)-1], "local:DELETE:workspace", key, func(ctx context.Context) error {
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
		http.Error(w, "local authority revoked", 403)
	case errors.Is(err, configstore.ErrStaleRevision):
		writeWorkspaceJSON(w, 412, workspaces.CommandError{Code: workspaces.Conflict, Message: "Workspace ETag changed; obtain a fresh preview"})
	case errors.Is(err, configstore.ErrIdempotencyConflict):
		writeWorkspaceJSON(w, 409, workspaces.CommandError{Code: workspaces.Conflict, Message: "idempotency_conflict"})
	case errors.Is(err, routing.ErrNotFound):
		writeWorkspaceJSON(w, 404, workspaces.CommandError{Code: workspaces.NotFound, Message: "Workspace not found"})
	case err != nil:
		h.writeError(w, err)
	default:
		writeWorkspaceJSON(w, 200, struct {
			Deleted bool `json:"deleted"`
		}{true})
	}
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
	expected, ok := reviewedWorkspaceMutation(w, r)
	if !ok {
		return
	}
	var spec workspaces.RouteSpec
	if !decodeWorkspaceJSON(w, r, &spec) {
		return
	}
	value, err := h.service.PutModelRouteChecked(r.Context(), workspaces.ReplaceRoute{
		Workspace: r.PathValue("workspace"), Route: r.PathValue("route"), Spec: spec,
	}, expected, func(ctx context.Context) error {
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
		writeWorkspaceJSON(w, http.StatusPreconditionFailed, workspaces.CommandError{Code: workspaces.Conflict, Message: "Workspace changed; reload route before editing"})
	case err != nil:
		h.writeError(w, err)
	default:
		w.Header().Set("ETag", `"`+value.ManagementETag+`"`)
		writeWorkspaceJSON(w, http.StatusOK, value)
	}
}

func decodeWorkspaceJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	if err := decodeOperatorJSONObject(w, r, target, "workspace command"); err != nil {
		writeWorkspaceJSON(w, operatorJSONErrorStatus(err), workspaces.CommandError{Code: workspaces.InvalidArgument, Message: "invalid command body: " + err.Error()})
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
	w.Header().Set("Cache-Control", "no-store")
	raw, err := json.Marshal(value)
	if err != nil {
		http.Error(w, fmt.Sprintf("encode response: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(append(raw, '\n'))
}

func writeOperatorJSON(w http.ResponseWriter, status int, value any) {
	writeWorkspaceJSON(w, status, value)
}

// Services are a real lifecycle, not projections inferred from nonempty routes.
func (h workspaceControlHandlers) listServices(w http.ResponseWriter, r *http.Request) {
	values, err := h.service.Services(r.Context(), r.PathValue("workspace"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeWorkspaceJSON(w, http.StatusOK, values)
}
func (h workspaceControlHandlers) enableService(w http.ResponseWriter, r *http.Request) {
	h.setService(w, r, true)
}
func (h workspaceControlHandlers) disableService(w http.ResponseWriter, r *http.Request) {
	h.setService(w, r, false)
}
func (h workspaceControlHandlers) setService(w http.ResponseWriter, r *http.Request, enabled bool) {
	if !IsLocalOperatorRequest(r) {
		http.Error(w, "trusted local operator context required", http.StatusForbidden)
		return
	}
	if r.URL.RawQuery != "" {
		http.Error(w, "Service change does not accept query parameters", http.StatusBadRequest)
		return
	}
	values := r.Header.Values("If-Match")
	if len(values) != 1 || len(values[0]) < 3 || values[0][0] != '"' || values[0][len(values[0])-1] != '"' || strings.ContainsAny(values[0][1:len(values[0])-1], "\"\\, \t\r\n") {
		http.Error(w, "one strong Workspace ETag required", http.StatusPreconditionRequired)
		return
	}
	var digest string
	if r.Body != nil && r.Body != http.NoBody {
		if !enabled {
			http.Error(w, "Disabling a Service has no request body", http.StatusBadRequest)
			return
		}
		// The shared Control decoder deliberately permits extension members.
		// The authority-expansion review does not: only an exact digest may
		// authorize a new Service for existing Principals or relay clients.
		var request map[string]json.RawMessage
		if !decodeWorkspaceJSON(w, r, &request) {
			return
		}
		for key := range request {
			if key != "review_digest" {
				http.Error(w, "unknown Service activation member", http.StatusBadRequest)
				return
			}
		}
		if raw, provided := request["review_digest"]; provided {
			if err := json.Unmarshal(raw, &digest); err != nil || len(digest) != 64 {
				http.Error(w, "review_digest must be a 64-character lowercase hex digest", http.StatusBadRequest)
				return
			}
			for _, digit := range digest {
				if !(digit >= '0' && digit <= '9' || digit >= 'a' && digit <= 'f') {
					http.Error(w, "review_digest must be a lowercase hex digest", http.StatusBadRequest)
					return
				}
			}
		}
	}
	value, err := h.service.SetServiceReviewed(r.Context(), r.PathValue("workspace"), r.PathValue("service"), enabled, strings.Trim(values[0], `"`), digest, func(ctx context.Context) error {
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
	case errors.Is(err, workspaces.ErrServiceReviewRequired):
		writeWorkspaceJSON(w, http.StatusPreconditionRequired, workspaces.CommandError{Code: workspaces.Conflict, Message: "Service activation expands authority; confirm the current impact review"})
	case errors.Is(err, workspaces.ErrServiceReviewChanged):
		writeWorkspaceJSON(w, http.StatusConflict, workspaces.CommandError{Code: workspaces.Conflict, Message: "Service activation impact changed; reload Services to review again"})
	case errors.Is(err, configstore.ErrStaleRevision):
		writeWorkspaceJSON(w, http.StatusPreconditionFailed, workspaces.CommandError{Code: workspaces.Conflict, Message: "Workspace ETag changed"})
	case err != nil:
		h.writeError(w, err)
	default:
		w.Header().Set("ETag", `"`+value.ManagementETag+`"`)
		w.Header().Set("Cache-Control", "no-store")
		writeWorkspaceJSON(w, http.StatusOK, value)
	}
}

// listModelRoutes is the canonical, redacted Workspace Models read surface.
// Full desired drafts must be fetched via the route resource, not reconstructed
// from these projections when editing.
func (h workspaceControlHandlers) listModelRoutes(w http.ResponseWriter, r *http.Request) {
	workspace, err := h.service.GetWorkspace(r.Context(), r.PathValue("workspace"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	w.Header().Set("ETag", `"`+workspace.ManagementETag+`"`)
	writeWorkspaceJSON(w, http.StatusOK, workspace.Routes)
}

// previewModelMatch asks the production routing aggregate, not a second browser
// matcher. This is a pure query: it never contacts an upstream provider.
func (h workspaceControlHandlers) previewModelMatch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	if len(query) != 1 || len(query["model"]) != 1 || query.Get("model") == "" {
		http.Error(w, "one nonempty model query is required", http.StatusBadRequest)
		return
	}
	matched, err := h.service.PreviewModelMatch(r.Context(), r.PathValue("workspace"), query.Get("model"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeWorkspaceJSON(w, http.StatusOK, matched)
}

// reorderModelRoutes changes only first-match precedence. The transaction
// performs a second admission check under the durable Workspace publication lock.
func (h workspaceControlHandlers) reorderModelRoutes(w http.ResponseWriter, r *http.Request) {
	expected, ok := reviewedWorkspaceMutation(w, r)
	if !ok {
		return
	}
	var request struct {
		Order []string `json:"order"`
	}
	if !decodeWorkspaceJSON(w, r, &request) {
		return
	}
	if request.Order == nil {
		http.Error(w, "order array is required", http.StatusBadRequest)
		return
	}
	value, err := h.service.SetRouteOrderChecked(r.Context(), r.PathValue("workspace"), request.Order, expected, func(ctx context.Context) error {
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
		writeWorkspaceJSON(w, http.StatusPreconditionFailed, workspaces.CommandError{Code: workspaces.Conflict, Message: "Workspace changed; reload route order"})
	case err != nil:
		h.writeError(w, err)
	default:
		w.Header().Set("ETag", `"`+value.ManagementETag+`"`)
		w.Header().Set("Cache-Control", "no-store")
		writeWorkspaceJSON(w, http.StatusOK, value)
	}
}

// reviewedWorkspaceMutation unifies strong If-Match/admission on canonical
// Connection-owned Search and MCP deletion paths.
func reviewedWorkspaceMutation(w http.ResponseWriter, r *http.Request) (string, bool) {
	if !IsLocalOperatorRequest(r) {
		http.Error(w, "trusted local operator context required", http.StatusForbidden)
		return "", false
	}
	if r.URL.RawQuery != "" {
		http.Error(w, "unrecognized query parameters", http.StatusBadRequest)
		return "", false
	}
	values := r.Header.Values("If-Match")
	if len(values) != 1 || len(values[0]) < 3 || values[0][0] != '"' || values[0][len(values[0])-1] != '"' || strings.ContainsAny(values[0][1:len(values[0])-1], "\"\\, \t\r\n") {
		http.Error(w, "one strong Workspace If-Match required", http.StatusPreconditionRequired)
		return "", false
	}
	return values[0][1 : len(values[0])-1], true
}
func (h workspaceControlHandlers) changeSearchRouteChecked(w http.ResponseWriter, r *http.Request, connectionID string, fallback []string) {
	expected, ok := reviewedWorkspaceMutation(w, r)
	if !ok {
		return
	}
	value, err := h.service.ChangeSearchRouteChecked(r.Context(), r.PathValue("workspace"), connectionID, fallback, expected, func(ctx context.Context) error {
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
		writeWorkspaceJSON(w, http.StatusPreconditionFailed, workspaces.CommandError{Code: workspaces.Conflict, Message: "Workspace changed; review current state"})
	case err != nil:
		h.writeError(w, err)
	default:
		w.Header().Set("ETag", `"`+value.ManagementETag+`"`)
		writeWorkspaceJSON(w, http.StatusOK, value)
	}
}
func (h workspaceControlHandlers) deleteMCPMountChecked(w http.ResponseWriter, r *http.Request) {
	expected, ok := reviewedWorkspaceMutation(w, r)
	if !ok {
		return
	}
	value, err := h.service.DeleteMCPMountChecked(r.Context(), r.PathValue("workspace"), r.PathValue("alias"), expected, func(ctx context.Context) error {
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
		writeWorkspaceJSON(w, http.StatusPreconditionFailed, workspaces.CommandError{Code: workspaces.Conflict, Message: "Workspace changed; review current state"})
	case err != nil:
		h.writeError(w, err)
	default:
		w.Header().Set("ETag", `"`+value.ManagementETag+`"`)
		writeWorkspaceJSON(w, http.StatusOK, value)
	}
}

// Canonical Models deletion requires the same local admission and strong
// revision as route authoring. There is no default-route replacement decision.
func (h workspaceControlHandlers) deleteModelRouteChecked(w http.ResponseWriter, r *http.Request) {
	expected, ok := reviewedWorkspaceMutation(w, r)
	if !ok {
		return
	}
	if r.ContentLength != 0 {
		http.Error(w, "route deletion does not take a body", http.StatusBadRequest)
		return
	}
	value, err := h.service.DeleteModelRouteChecked(r.Context(), r.PathValue("workspace"), r.PathValue("route"), expected, func(ctx context.Context) error {
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
		writeWorkspaceJSON(w, http.StatusPreconditionFailed, workspaces.CommandError{Code: workspaces.Conflict, Message: "Workspace changed; review current Models routes"})
	case err != nil:
		h.writeError(w, err)
	default:
		w.Header().Set("ETag", `"`+value.ManagementETag+`"`)
		writeWorkspaceJSON(w, http.StatusOK, value)
	}
}
