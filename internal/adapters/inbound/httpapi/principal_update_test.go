package httpapi

import (
	"encoding/json"
	accessapp "github.com/swobuforge/swobu/internal/app/operator/access"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPrincipalLabelUpdateRequiresExactRevisionAndPreservesAuthority(t *testing.T) {
	handler := accessHandler(t)
	created, err := handler.service.Create(t.Context(), "before", "prod")
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/principals/" + created.Access.ID
	read := httptest.NewRecorder()
	handler.ServeHTTP(read, permissionOperatorRequest(httptest.NewRequest(http.MethodGet, path, nil)))
	for _, probe := range []struct {
		revision string
		want     int
	}{{"", 428}, {`"stale"`, 412}, {read.Header().Get("ETag"), 200}} {
		req := permissionOperatorRequest(httptest.NewRequest(http.MethodPatch, path, strings.NewReader(`{"label":"after"}`)))
		req.Header.Set("Content-Type", "application/json")
		if probe.revision != "" {
			req.Header.Set("If-Match", probe.revision)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code != probe.want {
			t.Fatalf("label update=%d want=%d", response.Code, probe.want)
		}
	}
	principal, err := handler.service.GetPrincipal(t.Context(), created.Access.ID)
	if err != nil || principal.Label != "after" || len(principal.Permissions) != 1 {
		t.Fatal("label update changed authority")
	}
	credentials, err := handler.service.ListCredentials(t.Context(), created.Access.ID)
	if err != nil || len(credentials) != 1 || credentials[0] != created.Credential {
		t.Fatal("label update changed credentials")
	}
}

func TestPrincipalLabelInvalidOrUntrustedUpdateDoesNotPublish(t *testing.T) {
	handler := accessHandler(t)
	created, err := handler.service.Create(t.Context(), "before", "prod")
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/principals/" + created.Access.ID
	before, err := handler.service.GetPrincipal(t.Context(), created.Access.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, probe := range []struct {
		name, query, revision, body string
		trusted                     bool
		want                        int
	}{
		{"untrusted", "", `"` + before.Revision + `"`, `{"label":"after"}`, false, 403},
		{"unsupported preview", "?dry_run=true", `"` + before.Revision + `"`, `{"label":"after"}`, true, 400},
		{"weak revision", "", `W/"` + before.Revision + `"`, `{"label":"after"}`, true, 400},
		{"wildcard revision", "", "*", `{"label":"after"}`, true, 400},
		{"multiple revisions", "", `"one","two"`, `{"label":"after"}`, true, 400},
		{"empty label", "", `"` + before.Revision + `"`, `{"label":""}`, true, 400},
		{"wrong label type", "", `"` + before.Revision + `"`, `{"label":false}`, true, 400},
		{"trailing body", "", `"` + before.Revision + `"`, `{"label":"after"}{}`, true, 400},
	} {
		t.Run(probe.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPatch, path+probe.query, strings.NewReader(probe.body))
			if probe.trusted {
				req = permissionOperatorRequest(req)
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("If-Match", probe.revision)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			if response.Code != probe.want {
				t.Fatalf("status=%d want=%d", response.Code, probe.want)
			}
			current, err := handler.service.GetPrincipal(t.Context(), created.Access.ID)
			if err != nil || current.Label != before.Label || current.Revision != before.Revision {
				t.Fatal("rejected update published identity change")
			}
		})
	}
}

func TestPrincipalRevocationPreviewApplyAndExactRetry(t *testing.T) {
	handler := accessHandler(t)
	created, err := handler.service.Create(t.Context(), "retained", "prod")
	if err != nil {
		t.Fatal(err)
	}
	principal, err := handler.service.GetPrincipal(t.Context(), created.Access.ID)
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/principals/" + principal.ID
	revision := `"` + principal.Revision + `"`
	send := func(query, etag, key string, trusted bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodDelete, path+query, strings.NewReader(`{}`))
		if trusted {
			req = permissionOperatorRequest(req)
		}
		req.Header.Set("Content-Type", "application/json")
		if etag != "" {
			req.Header.Set("If-Match", etag)
		}
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	for _, probe := range []struct {
		query, etag, key string
		trusted          bool
		want             int
	}{
		{"", revision, "retry", false, 403}, {"", "", "retry", true, 428},
		{"", `W/"stale"`, "retry", true, 400}, {"", `"stale"`, "retry", true, 412},
		{"?dry_run=invalid", revision, "retry", true, 400}, {"", revision, "", true, 400},
	} {
		response := send(probe.query, probe.etag, probe.key, probe.trusted)
		if response.Code != probe.want {
			t.Fatalf("rejected revocation=%d want=%d", response.Code, probe.want)
		}
		current, err := handler.service.GetPrincipal(t.Context(), principal.ID)
		if err != nil || current.Revision != principal.Revision || current.Status != principal.Status {
			t.Fatal("rejected revocation changed authority")
		}
	}
	preview := send("?dry_run=true", revision, "", true)
	var impact accessapp.PrincipalRevocation
	if preview.Code != 200 || json.Unmarshal(preview.Body.Bytes(), &impact) != nil || impact.CredentialCount != 1 || len(impact.Principal.Permissions) != 1 || impact.Principal.Status != "active" || preview.Header().Get("ETag") != revision {
		t.Fatal("preview lost impact or revision")
	}
	first := send("", revision, "retry", true)
	var applied accessapp.PrincipalRevocation
	if first.Code != 200 || json.Unmarshal(first.Body.Bytes(), &applied) != nil || applied.Replayed || applied.Principal.Status != "revoked" || applied.CredentialCount != 0 || first.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("revocation acknowledgement invalid")
	}
	replay := send("", revision, "retry", true)
	if replay.Code != 200 || json.Unmarshal(replay.Body.Bytes(), &applied) != nil || !applied.Replayed || applied.Principal.Status != "revoked" {
		t.Fatal("exact replay failed")
	}
	if conflict := send("", first.Header().Get("ETag"), "retry", true); conflict.Code != 409 {
		t.Fatal("changed intent replay did not conflict")
	}
	if stale := send("", revision, "new-key", true); stale.Code != 412 {
		t.Fatal("fresh stale consent accepted")
	}
	credentials, err := handler.service.ListCredentials(t.Context(), principal.ID)
	if err != nil || len(credentials) != 0 {
		t.Fatal("revocation retained credential")
	}
	if strings.Contains(first.Body.String(), created.Token) || strings.Contains(replay.Body.String(), "revocation_receipts") {
		t.Fatal("secret or receipt leaked")
	}
}

func TestLegacyPrincipalRevocationRoutesAreAbsentAndCannotPublish(t *testing.T) {
	handler := accessHandler(t)
	created, err := handler.service.Create(t.Context(), "retained", "prod")
	if err != nil {
		t.Fatal(err)
	}
	before, err := handler.service.GetPrincipal(t.Context(), created.Access.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, prefix := range []string{"/_swobu", "/api/v1"} {
		req := permissionOperatorRequest(httptest.NewRequest(http.MethodPost, prefix+"/access/"+created.Access.ID+"/revoke", strings.NewReader(`{}`)))
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code != 404 {
			t.Fatalf("retired route status=%d", response.Code)
		}
		current, err := handler.service.GetPrincipal(t.Context(), created.Access.ID)
		if err != nil || current.Status != before.Status || current.Revision != before.Revision {
			t.Fatal("retired route changed authority")
		}
		credentials, err := handler.service.ListCredentials(t.Context(), created.Access.ID)
		if err != nil || len(credentials) != 1 {
			t.Fatal("retired route revoked credential")
		}
	}
}

func TestLegacyCombinedAccessCreationCannotMintIdentityPermissionOrSecret(t *testing.T) {
	handler := accessHandler(t)
	before := handler.service.ListPrincipals(t.Context())
	for _, path := range []string{"/_swobu/access", "/api/v1/access"} {
		req := permissionOperatorRequest(httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"name":"combined","workspace":"prod"}`)))
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		want := http.StatusNotFound
		if response.Code != want || len(handler.service.ListPrincipals(t.Context())) != len(before) || strings.Contains(response.Body.String(), "swak_") {
			t.Fatal("retired combined creation minted authority")
		}
	}
}
