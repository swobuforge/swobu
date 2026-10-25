package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/accessstate"
	accessapp "github.com/swobuforge/swobu/internal/app/operator/access"
	operatorclient "github.com/swobuforge/swobu/internal/app/operator/client"
	"github.com/swobuforge/swobu/internal/app/operator/routebindings"
	"github.com/swobuforge/swobu/internal/app/operator/workspaces"
	"github.com/swobuforge/swobu/internal/configstore"
	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/routing"
)

func permissionOperatorRequest(r *http.Request) *http.Request {
	return r.WithContext(contextWithAuthority(r, admittedAuthority(func(routing.WorkspaceSlug) (accessidentity.ID, error) { return accessidentity.Local, nil })))
}

func TestPermissionApplyAndReplayRepeatTransactionAdmission(t *testing.T) {
	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		for _, replay := range []bool{false, true} {
			t.Run(method+strconv.FormatBool(replay), func(t *testing.T) {
				handler := accessHandler(t)
				created, err := handler.service.Create(t.Context(), "transaction-admission", "prod")
				if err != nil {
					t.Fatal(err)
				}
				revision := permissionTestRevision(t, handler, created.Access.ID)
				request := func() *http.Request {
					req := httptest.NewRequest(method, "/api/v1/workspaces/prod/permissions/"+created.Access.ID, strings.NewReader(`{"action":"use","expires_at":"2000-01-01T00:00:00Z"}`))
					req.Header.Set("Content-Type", "application/json")
					req.Header.Set("If-Match", `"`+revision+`"`)
					req.Header.Set("Idempotency-Key", "transaction-admission-key")
					return req
				}
				if replay {
					response := httptest.NewRecorder()
					handler.ServeHTTP(response, permissionOperatorRequest(request()))
					if response.Code != 200 {
						t.Fatal("setup apply failed")
					}
				}
				before := permissionTestRevision(t, handler, created.Access.ID)
				checks := 0
				req := request()
				req = req.WithContext(contextWithAuthority(req, admittedAuthority(func(routing.WorkspaceSlug) (accessidentity.ID, error) {
					checks++
					if checks >= 3 {
						return accessidentity.ID{}, errors.New("authority withdrawn before transaction")
					}
					return accessidentity.Local, nil
				})))
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, req)
				if response.Code != 403 {
					t.Fatalf("transaction denial status=%d checks=%d", response.Code, checks)
				}
				if permissionTestRevision(t, handler, created.Access.ID) != before {
					t.Fatal("transaction denial changed authority")
				}
			})
		}
	}
}

func TestPermissionApplyRequiresOneBoundedRetryKey(t *testing.T) {
	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		for name, keys := range map[string][]string{"missing": nil, "empty": {""}, "multiple": {"one", "two"}, "space": {"not stable"}, "tab": {"not\tstable"}, "too-long": {strings.Repeat("k", 257)}} {
			t.Run(method+"/"+name, func(t *testing.T) {
				handler := accessHandler(t)
				created, err := handler.service.Create(t.Context(), "key-proof", "prod")
				if err != nil {
					t.Fatal(err)
				}
				revision := permissionTestRevision(t, handler, created.Access.ID)
				before, err := json.Marshal(handler.service.ListPrincipals(t.Context()))
				if err != nil {
					t.Fatal(err)
				}
				req := httptest.NewRequest(method, "/api/v1/workspaces/prod/permissions/"+created.Access.ID, strings.NewReader(`{"action":"use","expires_at":"2000-01-01T00:00:00Z"}`))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("If-Match", `"`+revision+`"`)
				for _, key := range keys {
					req.Header.Add("Idempotency-Key", key)
				}
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, permissionOperatorRequest(req))
				if response.Code != 400 {
					t.Fatalf("invalid key status=%d want=400", response.Code)
				}
				after, err := json.Marshal(handler.service.ListPrincipals(t.Context()))
				if err != nil {
					t.Fatal(err)
				}
				if permissionTestRevision(t, handler, created.Access.ID) != revision || string(before) != string(after) {
					t.Fatal("invalid retry key changed authority")
				}
				// Reusing the first valid member of an ambiguous header for
				// opposite intent proves rejection did not reserve that receipt.
				if name == "multiple" {
					correctedMethod := http.MethodPut
					if method == http.MethodPut {
						correctedMethod = http.MethodDelete
					}
					corrected := httptest.NewRequest(correctedMethod, "/api/v1/workspaces/prod/permissions/"+created.Access.ID, strings.NewReader(`{}`))
					corrected.Header.Set("Content-Type", "application/json")
					corrected.Header.Set("If-Match", `"`+revision+`"`)
					corrected.Header.Set("Idempotency-Key", keys[0])
					response = httptest.NewRecorder()
					handler.ServeHTTP(response, permissionOperatorRequest(corrected))
					if response.Code != 200 {
						t.Fatalf("rejected ambiguous key reserved receipt: status=%d", response.Code)
					}
				}
			})
		}
	}
}

func TestPermissionRetryKeyEnvelopeBoundaryAndPreview(t *testing.T) {
	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		handler := accessHandler(t)
		created, err := handler.service.Create(t.Context(), "key-boundary", "prod")
		if err != nil {
			t.Fatal(err)
		}
		revision := permissionTestRevision(t, handler, created.Access.ID)
		for _, query := range []string{"?dry_run=true", "", ""} {
			req := httptest.NewRequest(method, "/api/v1/workspaces/prod/permissions/"+created.Access.ID+query, strings.NewReader(`{}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("If-Match", `"`+revision+`"`)
			if query == "" {
				req.Header.Set("Idempotency-Key", strings.Repeat("k", 256))
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, permissionOperatorRequest(req))
			want := 200
			if query != "" {
				want = 200
			}
			if response.Code != want {
				t.Fatalf("query=%s status=%d want=%d", query, response.Code, want)
			}
		}
	}
}

func TestPermissionReadReturnsRelationshipWithoutIssuance(t *testing.T) {
	handler := accessHandler(t)
	created, err := handler.service.Create(t.Context(), "read-proof", "prod")
	if err != nil {
		t.Fatal(err)
	}
	revision := permissionTestRevision(t, handler, created.Access.ID)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/prod/permissions/"+created.Access.ID, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, permissionOperatorRequest(request))
	if response.Code != 200 {
		t.Fatalf("permission GET status=%d want=200", response.Code)
	}
	if response.Header().Get("ETag") != `"`+revision+`"` {
		t.Fatal("read revision not bound to response")
	}
	if permissionTestRevision(t, handler, created.Access.ID) != revision {
		t.Fatal("GET changed authority")
	}
	if strings.Contains(response.Body.String(), "swak_") || strings.Contains(response.Body.String(), "digest") {
		t.Fatal("read exposed credential material")
	}
}

func TestPermissionReadLifecycleThroughAdmittedCanonicalClient(t *testing.T) {
	handler := accessHandler(t)
	created, err := handler.service.Create(t.Context(), "read-lifecycle", "prod")
	if err != nil {
		t.Fatal(err)
	}
	admission, err := openTestAccessState(filepath.Join(t.TempDir(), "admission.json"))
	if err != nil {
		t.Fatal(err)
	}
	workspace, _ := routing.ParseWorkspaceSlug("prod")
	_, credential, err := admission.Create("consumer", workspace)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewLocalAdmissionHandler(handler, admission, nil))
	defer server.Close()
	client := operatorclient.New(server.Client(), server.URL)
	state, err := client.GetWorkspacePermission(t.Context(), "prod", created.Access.ID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Permission == nil || !state.Allowed {
		t.Fatal("initial grant not inspectable")
	}
	expiry := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := handler.service.ApplyPermissionChecked(t.Context(), created.Access.ID, "prod", state.Revision, "read-expiry", true, func() error { return nil }, expiry); err != nil {
		t.Fatal(err)
	}
	state, err = client.GetWorkspacePermission(t.Context(), "prod", created.Access.ID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Permission == nil || state.Permission.ExpiresAt == nil || !state.Permission.ExpiresAt.Equal(expiry) || state.Allowed {
		t.Fatal("expired relationship read is untruthful")
	}
	if err := handler.service.RevokePermission(t.Context(), created.Access.ID, "prod"); err != nil {
		t.Fatal(err)
	}
	state, err = client.GetWorkspacePermission(t.Context(), "prod", created.Access.ID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Permission != nil || state.Allowed {
		t.Fatal("absent grant read is untruthful")
	}
	for _, query := range []string{"", "?dry_run=true"} {
		req, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/workspaces/prod/permissions/"+created.Access.ID+query, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+credential.Token)
		response, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 403 {
			t.Fatalf("managed permission read status=%d", response.StatusCode)
		}
	}
	if _, err := client.GetWorkspacePermission(t.Context(), "absent", created.Access.ID); !operatorclient.IsNotFound(err) {
		t.Fatalf("missing Workspace=%v", err)
	}
	if _, err := client.GetWorkspacePermission(t.Context(), "prod", accessidentity.NewManaged([16]byte{9}).String()); !operatorclient.IsNotFound(err) {
		t.Fatalf("missing Principal=%v", err)
	}
}

func TestPermissionManagementRejectsNonLocalResolvedIdentity(t *testing.T) {
	handler := accessHandler(t)
	created, err := handler.service.Create(t.Context(), "identity-admission", "prod")
	if err != nil {
		t.Fatal(err)
	}
	revision := permissionTestRevision(t, handler, created.Access.ID)
	request := func(method, query string) *http.Request {
		req := httptest.NewRequest(method, "/api/v1/workspaces/prod/permissions/"+created.Access.ID+query, strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("If-Match", `"`+revision+`"`)
		req.Header.Set("Idempotency-Key", "identity-admission-key")
		return req
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, permissionOperatorRequest(request(http.MethodDelete, "")))
	if response.Code != 200 {
		t.Fatal("local setup apply failed")
	}
	after := permissionTestRevision(t, handler, created.Access.ID)
	for _, identity := range []accessidentity.ID{accessidentity.NewManaged([16]byte{1}), accessidentity.NewManaged([16]byte{2}), {}} {
		for _, method := range []string{http.MethodPut, http.MethodDelete} {
			for _, query := range []string{"", "?dry_run=true"} {
				req := request(method, query)
				req = req.WithContext(contextWithAuthority(req, admittedAuthority(func(routing.WorkspaceSlug) (accessidentity.ID, error) { return identity, nil })))
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, req)
				if response.Code != 403 {
					t.Fatalf("identity=%s method=%s query=%s status=%d", identity.String(), method, query, response.Code)
				}
				if permissionTestRevision(t, handler, created.Access.ID) != after {
					t.Fatal("nonlocal management request changed authority")
				}
			}
		}
	}
}

func TestPermissionCanonicalClientPreviewApplyReplayAndStale(t *testing.T) {
	handler := accessHandler(t)
	created, err := handler.service.Create(t.Context(), "client-proof", "prod")
	if err != nil {
		t.Fatal(err)
	}
	admission, err := openTestAccessState(filepath.Join(t.TempDir(), "admission.json"))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewLocalAdmissionHandler(handler, admission, nil))
	defer server.Close()
	client := operatorclient.New(server.Client(), server.URL)
	revision := permissionTestRevision(t, handler, created.Access.ID)
	deadline := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	preview, err := client.PreviewWorkspacePermission(t.Context(), "prod", created.Access.ID, true, deadline)
	if err != nil {
		t.Fatal(err)
	}
	if permissionTestRevision(t, handler, created.Access.ID) != revision {
		t.Fatal("client preview changed authority")
	}
	reviewed, err := operatorclient.ReviewPermission(preview, "canonical-grant-key")
	if err != nil {
		t.Fatal(err)
	}
	applied, err := client.ApplyWorkspacePermission(t.Context(), reviewed)
	if err != nil {
		t.Fatal(err)
	}
	if applied.Replayed || applied.Permission == nil || applied.Permission.ExpiresAt == nil || !applied.Permission.ExpiresAt.Equal(deadline) || applied.Revision == preview.Revision {
		t.Fatal("client omitted revised grant result")
	}
	if got := handler.service.ListPrincipals(t.Context())[0].Permissions[0].ExpiresAt; got == nil || !got.Equal(deadline) {
		t.Fatal("client apply lost expiry")
	}
	revokePreview, err := client.PreviewWorkspacePermission(t.Context(), "prod", created.Access.ID, false, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	revoke, err := operatorclient.ReviewPermission(revokePreview, "canonical-revoke-key")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.ApplyWorkspacePermission(t.Context(), revoke); err != nil {
		t.Fatal(err)
	}
	replayed, err := client.ApplyWorkspacePermission(t.Context(), reviewed)
	if err != nil {
		t.Fatal(err)
	}
	if !replayed.Replayed || replayed.Permission != nil || replayed.Allowed {
		t.Fatal("client replay misrepresented revoked authority")
	}
	if len(handler.service.ListPrincipals(t.Context())[0].Permissions) != 0 {
		t.Fatal("old client replay restored permission")
	}
	stale, err := operatorclient.ReviewPermission(preview, "fresh-key-old-consent")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.ApplyWorkspacePermission(t.Context(), stale); err == nil {
		t.Fatal("stale consent applied")
	}
}

func TestPermissionDryRunCannotPublishAuthorityOrReceipt(t *testing.T) {
	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			handler := accessHandler(t)
			created, err := handler.service.Create(t.Context(), "preview", "prod")
			if err != nil {
				t.Fatal(err)
			}
			revision := permissionTestRevision(t, handler, created.Access.ID)
			before, err := json.Marshal(handler.service.ListPrincipals(t.Context()))
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(method, "/api/v1/workspaces/prod/permissions/"+created.Access.ID+"?dry_run=true", strings.NewReader(`{"action":"use","expires_at":"2000-01-01T00:00:00Z"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("If-Match", `"`+revision+`"`)
			req.Header.Set("Idempotency-Key", "preview-only-key")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, permissionOperatorRequest(req))
			if response.Code != 200 {
				t.Fatalf("preview status=%d want=200", response.Code)
			}
			after, err := json.Marshal(handler.service.ListPrincipals(t.Context()))
			if err != nil {
				t.Fatal(err)
			}
			if permissionTestRevision(t, handler, created.Access.ID) != revision || string(before) != string(after) {
				t.Fatal("dry run changed authority")
			}
			if response.Header().Get("ETag") != `"`+revision+`"` {
				t.Fatal("preview missing current ETag")
			}
			var preview map[string]any
			if err := json.Unmarshal(response.Body.Bytes(), &preview); err != nil {
				t.Fatal(err)
			}
			if preview["principal"] != created.Access.ID || preview["workspace"] != "prod" || preview["approval_class"] != "trusted_confirmation" {
				t.Fatal("preview does not bind impact")
			}
			// Opposite apply with the preview's key proves no receipt reservation.
			applyMethod := http.MethodPut
			if method == http.MethodPut {
				applyMethod = http.MethodDelete
			}
			apply := httptest.NewRequest(applyMethod, "/api/v1/workspaces/prod/permissions/"+created.Access.ID, strings.NewReader(`{}`))
			apply.Header.Set("Content-Type", "application/json")
			apply.Header.Set("If-Match", `"`+revision+`"`)
			apply.Header.Set("Idempotency-Key", "preview-only-key")
			response = httptest.NewRecorder()
			handler.ServeHTTP(response, permissionOperatorRequest(apply))
			if response.Code != 200 {
				t.Fatalf("preview reserved receipt status=%d", response.Code)
			}
		})
	}
}

func TestPermissionPreviewDiscoveryAndMalformedQuerySafety(t *testing.T) {
	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			handler := accessHandler(t)
			created, err := handler.service.Create(t.Context(), "query-preview", "prod")
			if err != nil {
				t.Fatal(err)
			}
			revision := permissionTestRevision(t, handler, created.Access.ID)
			for _, query := range []string{"?dry_run=maybe", "?dry_run=", "?dry_run=true&dry_run=false", "?dry_run=%zz", "?preview=true", "?dry_run=true;extra=true"} {
				req := httptest.NewRequest(method, "/api/v1/workspaces/prod/permissions/"+created.Access.ID+query, strings.NewReader(`{}`))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("If-Match", `"`+revision+`"`)
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, permissionOperatorRequest(req))
				if response.Code != 400 {
					t.Fatalf("query %q status=%d", query, response.Code)
				}
				if permissionTestRevision(t, handler, created.Access.ID) != revision {
					t.Fatal("invalid query mutated authority")
				}
			}
			for _, etag := range []string{"", `"stale"`} {
				req := httptest.NewRequest(method, "/api/v1/workspaces/prod/permissions/"+created.Access.ID+"?dry_run=true", strings.NewReader(`{}`))
				req.Header.Set("Content-Type", "application/json")
				if etag != "" {
					req.Header.Set("If-Match", etag)
				}
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, permissionOperatorRequest(req))
				want := 200
				if etag != "" {
					want = 412
				}
				if response.Code != want {
					t.Fatalf("preview etag=%q status=%d want=%d", etag, response.Code, want)
				}
			}
		})
	}
}

func TestPermissionPreviewRealAdmissionRejectsManagedCredential(t *testing.T) {
	handler := accessHandler(t)
	principal, err := handler.service.Create(t.Context(), "preview-target", "prod")
	if err != nil {
		t.Fatal(err)
	}
	state, err := openTestAccessState(filepath.Join(t.TempDir(), "admission.json"))
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := routing.ParseWorkspaceSlug("prod")
	if err != nil {
		t.Fatal(err)
	}
	_, credential, err := state.Create("preview-consumer", workspace)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewLocalAdmissionHandler(handler, state, nil))
	defer server.Close()
	revision := permissionTestRevision(t, handler, principal.Access.ID)
	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		for _, managed := range []bool{true, false} {
			req, err := http.NewRequest(method, server.URL+"/api/v1/workspaces/prod/permissions/"+principal.Access.ID+"?dry_run=true", strings.NewReader(`{}`))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Type", "application/json")
			if managed {
				req.Header.Set("Authorization", "Bearer "+credential.Token)
			}
			response, err := server.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			want := 200
			if managed {
				want = 403
			}
			if response.StatusCode != want {
				t.Fatalf("method=%s managed=%t status=%d", method, managed, response.StatusCode)
			}
			if permissionTestRevision(t, handler, principal.Access.ID) != revision {
				t.Fatal("preview mutated authority")
			}
		}
	}
}

func TestPermissionGrantExpiryIsNotSilentlyDiscarded(t *testing.T) {
	handler := accessHandler(t)
	created, err := handler.service.Create(t.Context(), "expiry-test", "prod")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/v1/workspaces/prod/permissions/"+created.Access.ID, strings.NewReader(`{"action":"use","expires_at":"2000-01-01T00:00:00Z"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("If-Match", `"`+permissionTestRevision(t, handler, created.Access.ID)+`"`)
	req.Header.Set("Idempotency-Key", "expiry-repro")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, permissionOperatorRequest(req))
	if response.Code != 200 {
		t.Fatalf("grant status=%d", response.Code)
	}
	if _, err := handler.service.IssueCredential(t.Context(), created.Access.ID, "prod"); !errors.Is(err, accessstate.ErrUnauthorized) {
		t.Fatalf("expired grant allowed credential issuance: %v", err)
	}
}

func TestPermissionExpiryValidationProjectionAndRetryIntent(t *testing.T) {
	handler := accessHandler(t)
	created, err := handler.service.Create(t.Context(), "expiry-wire", "prod")
	if err != nil {
		t.Fatal(err)
	}
	revision := permissionTestRevision(t, handler, created.Access.ID)
	request := func(body string, key string) int {
		req := httptest.NewRequest(http.MethodPut, "/api/v1/workspaces/prod/permissions/"+created.Access.ID, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("If-Match", `"`+revision+`"`)
		req.Header.Set("Idempotency-Key", key)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, permissionOperatorRequest(req))
		return response.Code
	}
	for _, value := range []string{`null`, `7`, `""`, `"tomorrow"`, `"0001-01-01T00:00:00Z"`, `"2026-10-31"`} {
		if status := request(`{"action":"use","expires_at":`+value+`}`, "expiry-wire-key"); status != 400 {
			t.Fatalf("invalid expiry status=%d", status)
		}
		if permissionTestRevision(t, handler, created.Access.ID) != revision {
			t.Fatal("invalid expiry changed authority")
		}
	}
	if status := request(`{"action":"use","expires_at":"2099-10-31T01:00:00+01:00"}`, "expiry-wire-key"); status != 200 {
		t.Fatalf("valid expiry=%d", status)
	}
	after := permissionTestRevision(t, handler, created.Access.ID)
	if status := request(`{"action":"use","expires_at":"2099-10-31T00:00:00Z"}`, "expiry-wire-key"); status != 200 {
		t.Fatalf("equivalent instant replay=%d", status)
	}
	if permissionTestRevision(t, handler, created.Access.ID) != after {
		t.Fatal("replay changed revision")
	}
	if status := request(`{"action":"use","expires_at":"2099-11-01T00:00:00Z"}`, "expiry-wire-key"); status != 409 {
		t.Fatalf("changed deadline=%d", status)
	}
	permission := handler.service.ListPrincipals(t.Context())[0].Permissions[0]
	if permission.ExpiresAt == nil || permission.ExpiresAt.Format(time.RFC3339) != "2099-10-31T00:00:00Z" {
		t.Fatal("projection lost canonical expiry")
	}
}

func TestPermissionGrantRejectsUnsupportedActionWithoutReceipt(t *testing.T) {
	for _, body := range []string{`{"action":"manage"}`, `{"action":"unknown"}`, `{"action":7}`, `{"action":null}`, `{"action":""}`} {
		t.Run(body, func(t *testing.T) {
			handler := accessHandler(t)
			created, err := handler.service.Create(t.Context(), "action-test", "prod")
			if err != nil {
				t.Fatal(err)
			}
			revision := permissionTestRevision(t, handler, created.Access.ID)
			request := func(payload string) *http.Request {
				req := httptest.NewRequest(http.MethodPut, "/api/v1/workspaces/prod/permissions/"+created.Access.ID, strings.NewReader(payload))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("If-Match", `"`+revision+`"`)
				req.Header.Set("Idempotency-Key", "action-validation-key")
				return permissionOperatorRequest(req)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request(body))
			if response.Code != 400 {
				t.Fatalf("unsupported action status=%d want=400", response.Code)
			}
			if permissionTestRevision(t, handler, created.Access.ID) != revision {
				t.Fatal("invalid action changed authority")
			}
			response = httptest.NewRecorder()
			handler.ServeHTTP(response, request(`{"action":"use"}`))
			if response.Code != 200 {
				t.Fatalf("invalid action consumed retry key: status=%d", response.Code)
			}
		})
	}
}

// admissionWithdrawalBody models admission changing while ingress consumes the
// command, without timing-dependent goroutines or credential material.
type admissionWithdrawalBody struct {
	io.Reader
	withdraw func()
}

func (body admissionWithdrawalBody) Read(p []byte) (int, error) {
	body.withdraw()
	return body.Reader.Read(p)
}

func TestPermissionMutationRejectsAdmissionWithdrawnDuringDecode(t *testing.T) {
	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		for _, replay := range []bool{false, true} {
			t.Run(method+"/replay="+strconv.FormatBool(replay), func(t *testing.T) {
				handler := accessHandler(t)
				created, err := handler.service.Create(t.Context(), "decode-admission", "prod")
				if err != nil {
					t.Fatal(err)
				}
				revision := permissionTestRevision(t, handler, created.Access.ID)
				request := func() *http.Request {
					req := httptest.NewRequest(method, "/api/v1/workspaces/prod/permissions/"+created.Access.ID, strings.NewReader("{}"))
					req.Header.Set("Content-Type", "application/json")
					req.Header.Set("If-Match", `"`+revision+`"`)
					req.Header.Set("Idempotency-Key", "decode-admission-retry")
					return req
				}
				if replay {
					response := httptest.NewRecorder()
					handler.ServeHTTP(response, permissionOperatorRequest(request()))
					if response.Code != 200 {
						t.Fatalf("setup apply status=%d", response.Code)
					}
				}
				beforeRevision := permissionTestRevision(t, handler, created.Access.ID)
				beforePrincipals, err := json.Marshal(handler.service.ListPrincipals(t.Context()))
				if err != nil {
					t.Fatal(err)
				}
				admitted := true
				req := request()
				req.Body = io.NopCloser(admissionWithdrawalBody{Reader: strings.NewReader("{}"), withdraw: func() { admitted = false }})
				req = req.WithContext(contextWithAuthority(req, admittedAuthority(func(routing.WorkspaceSlug) (accessidentity.ID, error) {
					if !admitted {
						return accessidentity.ID{}, errors.New("admission withdrawn during decode")
					}
					return accessidentity.Local, nil
				})))
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, req)
				if response.Code != 403 {
					t.Fatalf("withdrawn admission status=%d want=403", response.Code)
				}
				afterPrincipals, err := json.Marshal(handler.service.ListPrincipals(t.Context()))
				if err != nil {
					t.Fatal(err)
				}
				if permissionTestRevision(t, handler, created.Access.ID) != beforeRevision || string(afterPrincipals) != string(beforePrincipals) {
					t.Fatal("rejected command changed permission authority")
				}
				if !replay {
					// Opposite intent with the rejected key must remain available:
					// a rejection must not reserve or publish a retry receipt.
					valid := request()
					valid.Method = http.MethodDelete
					if method == http.MethodDelete {
						valid.Method = http.MethodPut
					}
					response = httptest.NewRecorder()
					handler.ServeHTTP(response, permissionOperatorRequest(valid))
					if response.Code != 200 {
						t.Fatalf("rejected key was consumed: status=%d", response.Code)
					}
				}
			})
		}
	}
}

func TestPermissionMutationAndReplayRejectUnadmittedAuthority(t *testing.T) {
	handler := accessHandler(t)
	created, err := handler.service.Create(t.Context(), "authority-test", "prod")
	if err != nil {
		t.Fatal(err)
	}
	revision := permissionTestRevision(t, handler, created.Access.ID)
	for _, admitted := range []bool{false, true, false} {
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/workspaces/prod/permissions/"+created.Access.ID, strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("If-Match", `"`+revision+`"`)
		req.Header.Set("Idempotency-Key", "authority-retry")
		if admitted {
			req = permissionOperatorRequest(req)
		}
		before := permissionTestRevision(t, handler, created.Access.ID)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		want := 403
		if admitted {
			want = 200
		}
		if response.Code != want {
			t.Fatalf("admitted=%t status=%d want=%d", admitted, response.Code, want)
		}
		if !admitted && permissionTestRevision(t, handler, created.Access.ID) != before {
			t.Fatal("unadmitted request changed authority")
		}
	}
	for _, authority := range []admittedAuthority{
		func(workspace routing.WorkspaceSlug) (accessidentity.ID, error) {
			if workspace.String() == "prod" {
				return accessidentity.Local, nil
			}
			return accessidentity.ID{}, errors.New("Workspace scope only")
		},
		func(routing.WorkspaceSlug) (accessidentity.ID, error) {
			return accessidentity.ID{}, errors.New("admission withdrawn")
		},
	} {
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/workspaces/prod/permissions/"+created.Access.ID, strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("If-Match", `"`+revision+`"`)
		req.Header.Set("Idempotency-Key", "authority-retry")
		req = req.WithContext(contextWithAuthority(req, authority))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code != 403 {
			t.Fatalf("nonoperator replay status=%d", response.Code)
		}
	}
}

func TestPermissionMutationRealAdmissionDeniesWorkspaceCredential(t *testing.T) {
	handler := accessHandler(t)
	principal, err := handler.service.Create(t.Context(), "target", "prod")
	if err != nil {
		t.Fatal(err)
	}
	state, err := openTestAccessState(filepath.Join(t.TempDir(), "admission.json"))
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := routing.ParseWorkspaceSlug("prod")
	if err != nil {
		t.Fatal(err)
	}
	_, credential, err := state.Create("consumer", workspace)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewLocalAdmissionHandler(handler, state, nil))
	defer server.Close()
	revision := permissionTestRevision(t, handler, principal.Access.ID)
	for _, managed := range []bool{true, false, true} {
		req, err := http.NewRequest(http.MethodDelete, server.URL+"/api/v1/workspaces/prod/permissions/"+principal.Access.ID, strings.NewReader("{}"))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("If-Match", `"`+revision+`"`)
		req.Header.Set("Idempotency-Key", "real-admission-retry")
		if managed {
			req.Header.Set("Authorization", "Bearer "+credential.Token)
		}
		response, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		want := 200
		if managed {
			want = 403
		}
		if response.StatusCode != want {
			t.Fatalf("managed=%t status=%d want=%d", managed, response.StatusCode, want)
		}
	}
}

func TestCanonicalPrincipalDetailAndCredentialMetadataAreReadOnly(t *testing.T) {
	handler := accessHandler(t)
	created, err := handler.service.Create(t.Context(), "metadata-proof", "prod")
	if err != nil {
		t.Fatal(err)
	}
	principalPath := "/api/v1/principals/" + created.Access.ID
	for _, path := range []string{principalPath, principalPath + "/credentials"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusForbidden {
			t.Fatalf("unadmitted metadata read status=%d", response.Code)
		}
	}
	read := func(path string, want int) []byte {
		t.Helper()
		req := permissionOperatorRequest(httptest.NewRequest(http.MethodGet, path, nil))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code != want {
			t.Fatalf("read %s status=%d want=%d", path, response.Code, want)
		}
		if want == http.StatusOK && response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("Principal metadata can be cached")
		}
		if strings.Contains(response.Body.String(), created.Token) || strings.Contains(response.Body.String(), "digest") || strings.Contains(response.Body.String(), "token") {
			t.Fatal("metadata disclosed secret material")
		}
		return response.Body.Bytes()
	}
	before := handler.service.Revision()
	var principal accessapp.Principal
	if err := json.Unmarshal(read(principalPath, 200), &principal); err != nil {
		t.Fatal(err)
	}
	if principal.ID != created.Access.ID || principal.Label != "metadata-proof" || principal.Status != "active" || len(principal.Permissions) != 1 {
		t.Fatal("Principal detail disagrees with authority")
	}
	var credentials []accessapp.Credential
	if err := json.Unmarshal(read(principalPath+"/credentials", 200), &credentials); err != nil {
		t.Fatal(err)
	}
	if len(credentials) != 1 || credentials[0] != created.Credential {
		t.Fatal("credential list disagrees with issuance metadata")
	}
	read("/api/v1/principals/"+accessidentity.NewManaged([16]byte{1}).String(), 404)
	read("/api/v1/principals/"+accessidentity.NewManaged([16]byte{1}).String()+"/credentials", 404)
	read(principalPath+"?dry_run=true", 400)
	read(principalPath+"/credentials?secret=true", 400)
	if handler.service.Revision() != before {
		t.Fatal("metadata inspection mutated Access state")
	}
	if err := handler.service.Revoke(t.Context(), created.Access.ID); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(read(principalPath, 200), &principal); err != nil {
		t.Fatal(err)
	}
	if principal.Status != "revoked" || principal.ID != created.Access.ID {
		t.Fatal("revocation erased historical Principal identity")
	}
	if strings.TrimSpace(string(read(principalPath+"/credentials", 200))) != "[]" {
		t.Fatal("revoked Principal retains usable credentials")
	}
}

func TestCanonicalPrincipalCreationDoesNotIssueAuthority(t *testing.T) {
	handler := accessHandler(t)
	for _, probe := range []struct {
		body     string
		key      string
		admitted bool
		want     int
	}{
		{`{"label":"denied"}`, "deny-key", false, 403},
		{`{"label":"missing-key"}`, "", true, 400},
		{`{"label":""}`, "empty-label", true, 400},
		{`{"label":null}`, "null-label", true, 400},
		{`{"label":42}`, "numeric-label", true, 400},
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/principals", strings.NewReader(probe.body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", probe.key)
		if probe.admitted {
			req = permissionOperatorRequest(req)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code != probe.want || len(handler.service.ListPrincipals(t.Context())) != 0 {
			t.Fatalf("invalid creation status=%d want=%d or identity published", response.Code, probe.want)
		}
	}
	create := func(label string, want int) accessapp.Principal {
		t.Helper()
		req := permissionOperatorRequest(httptest.NewRequest(http.MethodPost, "/api/v1/principals", strings.NewReader(`{"label":"`+label+`"}`)))
		req.Header.Set("Idempotency-Key", "principal-create-proof")
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code != want {
			t.Fatalf("Principal create status=%d want=%d", response.Code, want)
		}
		var principal accessapp.Principal
		if want == 201 || want == 200 {
			if err := json.Unmarshal(response.Body.Bytes(), &principal); err != nil {
				t.Fatal(err)
			}
			if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Location") != "/api/v1/principals/"+principal.ID {
				t.Fatal("creation metadata headers missing")
			}
		}
		return principal
	}
	principal := create("independent", 201)
	if principal.ID == "" || len(principal.Permissions) != 0 || principal.Label != "independent" {
		t.Fatal("Principal creation granted authority or lost metadata")
	}
	credentials, err := handler.service.ListCredentials(t.Context(), principal.ID)
	if err != nil || len(credentials) != 0 {
		t.Fatal("Principal creation issued credential")
	}
	if replay := create("independent", 200); replay.ID != principal.ID {
		t.Fatal("creation replay minted a new identity")
	}
	create("different", 409)
	if err := handler.service.Revoke(t.Context(), principal.ID); err != nil {
		t.Fatal(err)
	}
	if replay := create("independent", 200); replay.ID != principal.ID || replay.Status != "revoked" {
		t.Fatal("creation replay restored revoked identity")
	}
}

func accessHandler(t *testing.T) AccessControlHandler {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	configStore, err := configstore.OpenOrCreate(filepath.Join(dir, "swobu.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	workspaceService, err := workspaces.NewService(configStore, workspaces.Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = workspaceService.CreateWorkspaceChecked(t.Context(), workspaces.CreateWorkspace{Slug: "prod", InitialRoute: "chat", Target: workspaces.TargetDraft{ID: "tgt_access", Model: "gpt-5", Protocol: "responses", ConnectionID: "cn_fixture"}}, "fixture-operator", "fixture-36449", func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = configStore.Close() })
	state, _ := openTestAccessState(filepath.Join(t.TempDir(), "access.json"))
	service, err := accessapp.NewService(state, configStore, &routebindings.Coordinator{})
	if err != nil {
		t.Fatal(err)
	}
	return NewAccessControlHandler(service)
}

func TestCanonicalPrincipalListDoesNotRecoverSecrets(t *testing.T) {
	handler := accessHandler(t)
	created, err := handler.service.Create(t.Context(), "CI", "prod")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(created.Token, "swak_") {
		t.Fatalf("token=%q", created.Token)
	}
	list := httptest.NewRecorder()
	handler.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/v1/principals", nil))
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"id":"`+created.Access.ID+`"`) || strings.Contains(list.Body.String(), created.Token) {
		t.Fatalf("list=%s", list.Body.String())
	}
}

func TestAccessControlRequiresJSONMutationContentType(t *testing.T) {
	request := permissionOperatorRequest(httptest.NewRequest(http.MethodPost, "/api/v1/principals", strings.NewReader(`{"label":"CI"}`)))
	request.Header.Set("Idempotency-Key", "fixture-create")
	response := httptest.NewRecorder()
	accessHandler(t).ServeHTTP(response, request)
	if response.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status=%d", response.Code)
	}
}

func TestPermissionRetryKeyChangedIntentConflictsWithoutSecondMutation(t *testing.T) {
	handler := accessHandler(t)
	created, err := handler.service.Create(t.Context(), "retry-test", "prod")
	if err != nil {
		t.Fatal(err)
	}
	revision := permissionTestRevision(t, handler, created.Access.ID)
	request := func(method string) int {
		req := httptest.NewRequest(method, "/api/v1/workspaces/prod/permissions/"+created.Access.ID, strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("If-Match", `"`+revision+`"`)
		req.Header.Set("Idempotency-Key", "permission-http-retry")
		req = permissionOperatorRequest(req)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response.Code
	}
	if status := request(http.MethodDelete); status != 200 {
		t.Fatalf("first=%d", status)
	}
	after := permissionTestRevision(t, handler, created.Access.ID)
	if status := request(http.MethodDelete); status != 200 {
		t.Fatalf("replay=%d", status)
	}
	if status := request(http.MethodPut); status != 409 {
		t.Fatalf("changed intent=%d", status)
	}
	if permissionTestRevision(t, handler, created.Access.ID) != after {
		t.Fatal("changed intent restored permission")
	}
}

func TestPermissionMutationRequiresCurrentRevisionWithoutChangingAuthority(t *testing.T) {
	handler := accessHandler(t)
	created, err := handler.service.Create(t.Context(), "revision-test", "prod")
	if err != nil {
		t.Fatal(err)
	}
	stale := permissionTestRevision(t, handler, created.Access.ID)
	if err := handler.service.RevokePermission(t.Context(), created.Access.ID, "prod"); err != nil {
		t.Fatal(err)
	}
	before := permissionTestRevision(t, handler, created.Access.ID)
	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		for _, check := range []struct {
			header string
			status int
		}{{"", 428}, {`"` + stale + `"`, 412}} {
			req := httptest.NewRequest(method, "/api/v1/workspaces/prod/permissions/"+created.Access.ID, strings.NewReader("{}"))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("If-Match", check.header)
			req.Header.Set("Idempotency-Key", "revision-rejection-key")
			req = permissionOperatorRequest(req)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			if response.Code != check.status {
				t.Fatalf("%s If-Match=%q status=%d want=%d", method, check.header, response.Code, check.status)
			}
			if permissionTestRevision(t, handler, created.Access.ID) != before {
				t.Fatal("rejected mutation changed authority")
			}
		}
	}
	for _, header := range []string{"*", "W/\"stale\"", `"one", "two"`, `"bad space"`} {
		req := httptest.NewRequest(http.MethodPut, "/api/v1/workspaces/prod/permissions/"+created.Access.ID, strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("If-Match", header)
		req = permissionOperatorRequest(req)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code != 400 || permissionTestRevision(t, handler, created.Access.ID) != before {
			t.Fatalf("malformed %q status=%d changed=%t", header, response.Code, permissionTestRevision(t, handler, created.Access.ID) != before)
		}
	}
	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		req := httptest.NewRequest(method, "/api/v1/workspaces/prod/permissions/"+created.Access.ID, strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("If-Match", `"`+permissionTestRevision(t, handler, created.Access.ID)+`"`)
		req.Header.Set("Idempotency-Key", "revision-current-"+method)
		req = permissionOperatorRequest(req)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code != 200 {
			t.Fatalf("current %s status=%d", method, response.Code)
		}
		if permissionTestRevision(t, handler, created.Access.ID) == before {
			t.Fatal("current revision mutation was not published")
		}
		before = permissionTestRevision(t, handler, created.Access.ID)
	}
}

func TestAccessControlProjectsPrincipalsAtVersionedNodePath(t *testing.T) {
	handler := accessHandler(t)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/principals", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if !strings.HasPrefix(strings.TrimSpace(response.Body.String()), "[") {
		t.Fatalf("principal projection=%s", response.Body.String())
	}
}

func TestAccessControlCredentialMetadataIsProjectedThroughPrincipalResources(t *testing.T) {
	handler := accessHandler(t)
	result, err := handler.service.Create(t.Context(), "CI", "prod")
	if err != nil {
		t.Fatal(err)
	}
	list := httptest.NewRecorder()
	handler.ServeHTTP(list, permissionOperatorRequest(httptest.NewRequest(http.MethodGet, "/api/v1/principals/"+result.Access.ID+"/credentials", nil)))
	if list.Code != http.StatusOK || strings.Contains(list.Body.String(), result.Token) || !strings.Contains(list.Body.String(), result.Credential.ID) {
		t.Fatalf("canonical credential metadata status/body=%d/%s", list.Code, list.Body.String())
	}
	for _, prefix := range []string{"/api/v1/access/", "/_swobu/access/"} {
		revoke := httptest.NewRecorder()
		revokeReq := permissionOperatorRequest(httptest.NewRequest(http.MethodDelete, prefix+result.Access.ID+"/credentials/"+result.Credential.ID, strings.NewReader(`{}`)))
		revokeReq.Header.Set("Content-Type", "application/json")
		handler.ServeHTTP(revoke, revokeReq)
		if revoke.Code != http.StatusNotFound {
			t.Fatalf("legacy revoke status=%d body=%s", revoke.Code, revoke.Body.String())
		}
		credentials, err := handler.service.ListCredentials(t.Context(), result.Access.ID)
		if err != nil || len(credentials) != 1 || credentials[0] != result.Credential {
			t.Fatal("retired route withdrew credential")
		}
	}
}

func TestAccessControlDoesNotExposeDuplicateVersionedAccessCollection(t *testing.T) {
	response := httptest.NewRecorder()
	accessHandler(t).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/access", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("duplicate Access collection status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestAccessControlDoesNotProjectCredentialLifecycleAtUnversionedNodePath(t *testing.T) {
	response := httptest.NewRecorder()
	accessHandler(t).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/access", nil))
	if response.Code == http.StatusOK {
		t.Fatalf("unversioned route unexpectedly served: %s", response.Body.String())
	}
}

func TestAccessControlPermissionRoutesAreVersionedAndSeparateFromCredentials(t *testing.T) {
	handler := accessHandler(t)
	for _, tc := range []struct{ method, path string }{
		{http.MethodPut, "/api/v1/workspaces/prod/permissions/acc_missing"},
		{http.MethodDelete, "/api/v1/workspaces/prod/permissions/acc_missing"},
	} {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("If-Match", `"`+handler.service.Revision()+`"`)
		req = permissionOperatorRequest(req)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code != http.StatusBadRequest && response.Code != http.StatusNotFound {
			t.Fatalf("%s %s status=%d", tc.method, tc.path, response.Code)
		}
	}
}

func permissionTestRevision(t *testing.T, handler AccessControlHandler, principal string) string {
	t.Helper()
	state, err := handler.service.ReadPermission(principal, "prod")
	if err != nil {
		t.Fatal(err)
	}
	return state.Revision
}
