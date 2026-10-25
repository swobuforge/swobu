package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/swobuforge/swobu/internal/accessstate"
	accessapp "github.com/swobuforge/swobu/internal/app/operator/access"
	"github.com/swobuforge/swobu/internal/app/operator/workspaces"
	"github.com/swobuforge/swobu/internal/routing"
)

type AccessControlHandler struct {
	service accessapp.Service
	mux     *http.ServeMux
}

func NewAccessControlHandler(service accessapp.Service) AccessControlHandler {
	h := AccessControlHandler{service: service, mux: http.NewServeMux()}
	h.mux.HandleFunc("GET /api/v1/principals", h.listPrincipals)
	h.mux.HandleFunc("POST /api/v1/principals", h.createPrincipal)
	h.mux.HandleFunc("GET /api/v1/principals/{access}", h.readPrincipal)
	h.mux.HandleFunc("PATCH /api/v1/principals/{access}", h.updatePrincipal)
	h.mux.HandleFunc("DELETE /api/v1/principals/{access}", h.revokePrincipal)
	h.mux.HandleFunc("GET /api/v1/principals/{access}/credentials", h.listCredentials)
	h.mux.HandleFunc("POST /api/v1/principals/{access}/credentials", h.issue)
	h.mux.HandleFunc("DELETE /api/v1/principals/{access}/credentials/{credential}", h.revokeCredentialChecked)
	h.mux.HandleFunc("GET /api/v1/workspaces/{workspace}/permissions/{principal}", h.readPermission)
	h.mux.HandleFunc("PUT /api/v1/workspaces/{workspace}/permissions/{principal}", h.grantPermission)
	h.mux.HandleFunc("DELETE /api/v1/workspaces/{workspace}/permissions/{principal}", h.revokePermission)
	return h
}

func (h AccessControlHandler) readPermission(w http.ResponseWriter, r *http.Request) {
	if !IsLocalOperatorRequest(r) {
		http.Error(w, "trusted local operator context required", http.StatusForbidden)
		return
	}
	if r.URL.RawQuery != "" {
		http.Error(w, "permission read does not accept query parameters", http.StatusBadRequest)
		return
	}
	state, err := h.service.ReadPermission(r.PathValue("principal"), r.PathValue("workspace"))
	if err != nil {
		writeAccessError(w, err)
		return
	}
	w.Header().Set("ETag", `"`+state.Revision+`"`)
	w.Header().Set("Cache-Control", "no-store")
	writeWorkspaceJSON(w, http.StatusOK, state)
}

func (h AccessControlHandler) grantPermission(w http.ResponseWriter, r *http.Request) {
	if !IsLocalOperatorRequest(r) {
		http.Error(w, "trusted local operator context required", http.StatusForbidden)
		return
	}
	dryRun, ok := permissionDryRun(w, r)
	if !ok {
		return
	}
	if (!dryRun || len(r.Header.Values("If-Match")) != 0) && !permissionRevisionPrecondition(w, r) {
		return
	}
	var body struct {
		Action        json.RawMessage `json:"action"`
		ExpiresAt     json.RawMessage `json:"expires_at"`
		Services      json.RawMessage `json:"services"`
		RequestLimits json.RawMessage `json:"request_limits"`
	}
	if !decodeAccessBody(w, r, &body) {
		return
	}
	// Workspace permission has one supported action. Additive JSON members do
	// not make a known action field extensible or allow Node management here.
	// Omission retains the existing use-only command; explicit values are strict.
	if len(body.Action) != 0 {
		var action string
		if json.Unmarshal(body.Action, &action) != nil || action != "use" {
			http.Error(w, "Workspace permission action must be use", http.StatusBadRequest)
			return
		}
	}
	var expiry []time.Time
	if len(body.ExpiresAt) != 0 {
		var raw string
		if json.Unmarshal(body.ExpiresAt, &raw) != nil {
			http.Error(w, "permission expiry must be an RFC3339 timestamp", http.StatusBadRequest)
			return
		}
		expires, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil || expires.IsZero() {
			http.Error(w, "permission expiry must be a nonzero RFC3339 timestamp", http.StatusBadRequest)
			return
		}
		expiry = []time.Time{expires.UTC()}
	}
	var services []routing.ServiceKind
	if len(body.Services) != 0 {
		if string(body.Services) == "null" || json.Unmarshal(body.Services, &services) != nil || services == nil || len(services) == 0 {
			http.Error(w, "permission Services must be a nonempty list", http.StatusBadRequest)
			return
		}
		seen := map[routing.ServiceKind]bool{}
		for _, kind := range services {
			if _, err := routing.ParseServiceKind(string(kind)); err != nil || seen[kind] {
				http.Error(w, "permission Services contain an invalid or duplicate kind", http.StatusBadRequest)
				return
			}
			seen[kind] = true
		}
	}
	// An omitted request_limits field retains existing restrictions. Explicit
	// [] clears them, but null and malformed/noncanonical entries fail closed.
	var rates []accessstate.RequestRateLimit
	if len(body.RequestLimits) != 0 {
		decoder := json.NewDecoder(bytes.NewReader(body.RequestLimits))
		decoder.DisallowUnknownFields()
		if string(body.RequestLimits) == "null" || decoder.Decode(&rates) != nil || rates == nil || decoder.Decode(&struct{}{}) != io.EOF {
			http.Error(w, "request_limits must be an exact list of Service and requests_per_minute", http.StatusBadRequest)
			return
		}
		seen := map[routing.ServiceKind]bool{}
		for _, rate := range rates {
			if _, err := routing.ParseServiceKind(string(rate.Service)); err != nil || rate.RequestsPerMinute == 0 || rate.RequestsPerMinute > 100000 || seen[rate.Service] {
				http.Error(w, "invalid request limit", http.StatusBadRequest)
				return
			}
			seen[rate.Service] = true
		}
		for _, rate := range rates {
			if services != nil {
				found := false
				for _, service := range services {
					if service == rate.Service {
						found = true
						break
					}
				}
				if !found {
					http.Error(w, "request limit requires a granted Service", http.StatusBadRequest)
					return
				}
			}
		}
	}
	// Body consumption can outlive admission. Replay requires current authority
	// too; receipt possession must never bypass this post-decode check.
	if !IsLocalOperatorRequest(r) {
		http.Error(w, "trusted local operator context required", http.StatusForbidden)
		return
	}
	if dryRun {
		preview, err := h.service.PreviewPermissionServicesWithRates(r.PathValue("principal"), r.PathValue("workspace"), strings.Trim(r.Header.Get("If-Match"), `"`), true, services, rates, expiry...)
		h.writePermissionPreview(w, preview, err)
		return
	}
	if !permissionRetryKey(w, r) {
		return
	}
	result, err := h.service.ApplyPermissionServicesWithRatesChecked(r.Context(), r.PathValue("principal"), r.PathValue("workspace"), strings.Trim(r.Header.Get("If-Match"), `"`), r.Header.Get("Idempotency-Key"), true, permissionTransactionAdmission(r), services, rates, expiry...)
	if err != nil {
		writePermissionError(w, err)
		return
	}
	h.writePermissionMutation(w, result)
}

func (h AccessControlHandler) revokePermission(w http.ResponseWriter, r *http.Request) {
	if !IsLocalOperatorRequest(r) {
		http.Error(w, "trusted local operator context required", http.StatusForbidden)
		return
	}
	dryRun, ok := permissionDryRun(w, r)
	if !ok {
		return
	}
	if (!dryRun || len(r.Header.Values("If-Match")) != 0) && !permissionRevisionPrecondition(w, r) {
		return
	}
	if !decodeAccessBody(w, r, &struct{}{}) {
		return
	}
	// Repeat admission after potentially blocking body consumption, before both
	// new authority publication and committed receipt replay.
	if !IsLocalOperatorRequest(r) {
		http.Error(w, "trusted local operator context required", http.StatusForbidden)
		return
	}
	if dryRun {
		preview, err := h.service.PreviewPermission(r.PathValue("principal"), r.PathValue("workspace"), strings.Trim(r.Header.Get("If-Match"), `"`), false)
		h.writePermissionPreview(w, preview, err)
		return
	}
	if !permissionRetryKey(w, r) {
		return
	}
	result, err := h.service.ApplyPermissionChecked(r.Context(), r.PathValue("principal"), r.PathValue("workspace"), strings.Trim(r.Header.Get("If-Match"), `"`), r.Header.Get("Idempotency-Key"), false, permissionTransactionAdmission(r))
	if err != nil {
		writePermissionError(w, err)
		return
	}
	h.writePermissionMutation(w, result)
}

func (h AccessControlHandler) writePermissionMutation(w http.ResponseWriter, result accessapp.PermissionMutation) {
	w.Header().Set("ETag", `"`+result.Revision+`"`)
	w.Header().Set("Cache-Control", "no-store")
	writeWorkspaceJSON(w, http.StatusOK, result)
}

func (h AccessControlHandler) writePermissionPreview(w http.ResponseWriter, preview accessapp.PermissionPreview, err error) {
	if err != nil {
		writePermissionError(w, err)
		return
	}
	w.Header().Set("ETag", `"`+preview.Revision+`"`)
	writeWorkspaceJSON(w, http.StatusOK, preview)
}

// writePermissionError preserves the permission-specific recovery action when
// a reviewed relationship revision becomes stale. The operator must obtain a
// fresh preview; a generic resource message loses that actionable contract.
func writePermissionError(w http.ResponseWriter, err error) {
	if errors.Is(err, accessstate.ErrRevisionConflict) {
		writeWorkspaceJSON(w, http.StatusPreconditionFailed, workspaces.CommandError{Code: workspaces.Conflict, Message: "Permission revision changed; obtain a fresh preview"})
		return
	}
	writeAccessError(w, err)
}

// permissionDryRun accepts only the documented closed query. Ignoring malformed
// preview intent could turn an operator's effect-free review into publication.
func permissionDryRun(w http.ResponseWriter, r *http.Request) (bool, bool) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		http.Error(w, "malformed permission query", http.StatusBadRequest)
		return false, false
	}
	for key, values := range query {
		if key != "dry_run" || len(values) != 1 || (values[0] != "true" && values[0] != "false") {
			http.Error(w, "unsupported permission query", http.StatusBadRequest)
			return false, false
		}
	}
	return query.Get("dry_run") == "true", true
}

func permissionRevisionPrecondition(w http.ResponseWriter, r *http.Request) bool {
	if _, problem := StrongRevision(r.Header); problem != nil {
		WriteControlProblem(w, r, *problem)
		return false
	}
	return true
}

// permissionRetryKey uses the shared canonical transport envelope. The
// authority store persists only a hash, not the supplied wire key.
// Preview never reaches this admission or reserves transaction evidence.
func permissionRetryKey(w http.ResponseWriter, r *http.Request) bool {
	if _, problem := IdempotencyKey(r.Header); problem != nil {
		WriteControlProblem(w, r, *problem)
		return false
	}
	return true
}
func (h AccessControlHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}
func (h AccessControlHandler) listPrincipals(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("ETag", `"`+h.service.Revision()+`"`)
	writeWorkspaceJSON(w, http.StatusOK, h.service.ListPrincipals(r.Context()))
}
func (h AccessControlHandler) createPrincipal(w http.ResponseWriter, r *http.Request) {
	if !principalMetadataRead(w, r) || !permissionRetryKey(w, r) {
		return
	}
	var body struct {
		Label string `json:"label"`
	}
	if !decodeAccessBody(w, r, &body) {
		return
	}
	principal, replayed, err := h.service.CreatePrincipalChecked(r.Context(), body.Label, r.Header.Get("Idempotency-Key"), permissionTransactionAdmission(r))
	if err != nil {
		writeAccessError(w, err)
		return
	}
	w.Header().Set("Location", "/api/v1/principals/"+url.PathEscape(principal.ID))
	w.Header().Set("ETag", `"`+principal.Revision+`"`)
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	}
	writeWorkspaceJSON(w, status, principal)
}

// Metadata reads require admitted Node authority even when this handler is
// composed without the daemon's outer guard. Query flags cannot reveal secrets.
func principalMetadataRead(w http.ResponseWriter, r *http.Request) bool {
	if !IsLocalOperatorRequest(r) {
		http.Error(w, "trusted local operator context required", http.StatusForbidden)
		return false
	}
	if r.URL.RawQuery != "" {
		http.Error(w, "Principal metadata does not accept query parameters", http.StatusBadRequest)
		return false
	}
	w.Header().Set("Cache-Control", "no-store")
	return true
}

func (h AccessControlHandler) readPrincipal(w http.ResponseWriter, r *http.Request) {
	if !principalMetadataRead(w, r) {
		return
	}
	principal, err := h.service.GetPrincipal(r.Context(), r.PathValue("access"))
	if err != nil {
		writeAccessError(w, err)
		return
	}
	w.Header().Set("ETag", `"`+principal.Revision+`"`)
	writeWorkspaceJSON(w, http.StatusOK, principal)
}

func (h AccessControlHandler) updatePrincipal(w http.ResponseWriter, r *http.Request) {
	if !principalMetadataRead(w, r) {
		return
	}
	values := r.Header.Values("If-Match")
	if len(values) == 0 || (len(values) == 1 && values[0] == "") {
		http.Error(w, "Principal revision required", http.StatusPreconditionRequired)
		return
	}
	value := values[0]
	if len(values) != 1 || len(value) < 3 || value[0] != '"' || value[len(value)-1] != '"' || strings.ContainsAny(value[1:len(value)-1], "\"\\, \t\r\n") {
		http.Error(w, "one strong Principal ETag required", http.StatusBadRequest)
		return
	}
	var body struct {
		Label string `json:"label"`
	}
	if !decodeAccessBody(w, r, &body) {
		return
	}
	principal, err := h.service.UpdatePrincipalLabelChecked(r.Context(), r.PathValue("access"), body.Label, value[1:len(value)-1], permissionTransactionAdmission(r))
	if err != nil {
		writeAccessError(w, err)
		return
	}
	w.Header().Set("ETag", `"`+principal.Revision+`"`)
	writeWorkspaceJSON(w, http.StatusOK, principal)
}

func (h AccessControlHandler) listCredentials(w http.ResponseWriter, r *http.Request) {
	if !principalMetadataRead(w, r) {
		return
	}
	credentials, err := h.service.ListCredentials(r.Context(), r.PathValue("access"))
	if err != nil {
		writeAccessError(w, err)
		return
	}
	writeWorkspaceJSON(w, http.StatusOK, credentials)
}

// revokePrincipal uses the same transaction-owned impact and admission for
// preview/apply. Dry-run never reserves a key and is not trusted confirmation.
func (h AccessControlHandler) revokePrincipal(w http.ResponseWriter, r *http.Request) {
	if !IsLocalOperatorRequest(r) {
		http.Error(w, "trusted local operator context required", http.StatusForbidden)
		return
	}
	dryRun, ok := permissionDryRun(w, r)
	if !ok || !permissionRevisionPrecondition(w, r) {
		return
	}
	if !decodeAccessBody(w, r, &struct{}{}) {
		return
	}
	expected := strings.Trim(r.Header.Get("If-Match"), `"`)
	var result accessapp.PrincipalRevocation
	var err error
	if dryRun {
		result, err = h.service.PreviewPrincipalRevocation(r.PathValue("access"), expected, permissionTransactionAdmission(r))
	} else {
		if !permissionRetryKey(w, r) {
			return
		}
		result, err = h.service.ApplyPrincipalRevocation(r.PathValue("access"), expected, r.Header.Get("Idempotency-Key"), permissionTransactionAdmission(r))
	}
	if err != nil {
		writeAccessError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("ETag", `"`+result.Principal.Revision+`"`)
	writeWorkspaceJSON(w, http.StatusOK, result)
}
func (h AccessControlHandler) issue(w http.ResponseWriter, r *http.Request) {
	if !IsLocalOperatorRequest(r) {
		http.Error(w, "trusted local operator context required", http.StatusForbidden)
		return
	}
	if r.URL.RawQuery != "" {
		http.Error(w, "credential issuance does not accept query parameters", http.StatusBadRequest)
		return
	}
	if !permissionRetryKey(w, r) {
		return
	}
	var body struct {
		Workspace string `json:"workspace"`
	}
	if !decodeAccessBody(w, r, &body) {
		return
	}
	result, err := h.service.IssueCredentialChecked(r.PathValue("access"), body.Workspace, r.Header.Get("Idempotency-Key"), permissionTransactionAdmission(r))
	if err != nil {
		writeAccessError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	status := http.StatusCreated
	if result.SecretNotRecoverable {
		status = http.StatusOK
	}
	writeWorkspaceJSON(w, status, result)
}
func (h AccessControlHandler) revokeCredentialChecked(w http.ResponseWriter, r *http.Request) {
	if !IsLocalOperatorRequest(r) {
		http.Error(w, "trusted local operator context required", http.StatusForbidden)
		return
	}
	dryRun, ok := permissionDryRun(w, r)
	if !ok {
		return
	}
	if !dryRun && (!permissionRevisionPrecondition(w, r) || !permissionRetryKey(w, r)) {
		return
	}
	if !decodeAccessBody(w, r, &struct{}{}) {
		return
	}
	admit := permissionTransactionAdmission(r)
	if dryRun {
		impact, err := h.service.PreviewCredentialRevocation(r.PathValue("access"), r.PathValue("credential"), admit)
		if err != nil {
			writeAccessError(w, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("ETag", `"`+impact.Revision+`"`)
		writeWorkspaceJSON(w, http.StatusOK, impact)
		return
	}
	result, err := h.service.ApplyCredentialRevocation(r.Context(), r.PathValue("access"), r.PathValue("credential"), strings.Trim(r.Header.Get("If-Match"), `"`), r.Header.Get("Idempotency-Key"), admit)
	if err != nil {
		writeAccessError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	// This ETag describes the returned current Principal, not fresh credential
	// consent. Exact recovery always sends the original preview revision.
	w.Header().Set("ETag", `"`+result.Principal.Revision+`"`)
	writeWorkspaceJSON(w, http.StatusOK, result)
}

func decodeAccessBody(w http.ResponseWriter, r *http.Request, target any) bool {
	if err := decodeOperatorJSONObject(w, r, target, "access command"); err != nil {
		writeWorkspaceJSON(w, operatorJSONErrorStatus(err), map[string]string{"error": err.Error()})
		return false
	}
	return true
}
func writeAccessError(w http.ResponseWriter, err error) {
	// Permission recovery must preserve the semantic cause through the canonical
	// Operator client, rather than a string-valued envelope it cannot decode.
	if errors.Is(err, accessstate.ErrPermissionIdempotencyConflict) || errors.Is(err, accessstate.ErrCredentialIdempotencyConflict) || errors.Is(err, accessstate.ErrPrincipalIdempotencyConflict) || errors.Is(err, accessstate.ErrCredentialRevocationIdempotencyConflict) {
		writeWorkspaceJSON(w, http.StatusConflict, workspaces.CommandError{Code: workspaces.Conflict, Message: "idempotency_conflict"})
		return
	}
	if errors.Is(err, accessstate.ErrRevisionConflict) {
		writeWorkspaceJSON(w, http.StatusPreconditionFailed, workspaces.CommandError{Code: workspaces.Conflict, Message: "Resource revision changed; obtain fresh state before applying"})
		return
	}
	status := http.StatusBadRequest
	if errors.Is(err, errLocalAdmissionForbidden) {
		status = http.StatusForbidden
	}
	if errors.Is(err, routing.ErrNotFound) || errors.Is(err, accessstate.ErrNotFound) {
		status = http.StatusNotFound
	}
	if errors.Is(err, accessstate.ErrRevoked) {
		status = http.StatusConflict
	}
	writeWorkspaceJSON(w, status, map[string]string{"error": err.Error()})
}

// Only the built-in local closure reaches this callback in production. Managed
// credentials are denied before store entry; resolving them here would reenter
// the locked Access store. Trusted Node sessions must follow the same lock law.
func permissionTransactionAdmission(r *http.Request) func() error {
	return func() error {
		if !IsLocalOperatorRequest(r) {
			return errLocalAdmissionForbidden
		}
		return nil
	}
}
