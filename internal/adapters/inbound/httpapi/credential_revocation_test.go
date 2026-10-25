package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	accessapp "github.com/swobuforge/swobu/internal/app/operator/access"
	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestCredentialRevocationCheckedHTTPPreviewApplyAndReplay(t *testing.T) {
	handler := accessHandler(t)
	created, err := handler.service.Create(t.Context(), "retained", "prod")
	if err != nil {
		t.Fatal(err)
	}
	sibling, err := handler.service.IssueCredential(t.Context(), created.Access.ID, "prod")
	if err != nil {
		t.Fatal(err)
	}
	before, err := handler.service.GetPrincipal(t.Context(), created.Access.ID)
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/principals/" + created.Access.ID + "/credentials/" + created.Credential.ID
	send := func(query, revision, key, body string, trusted bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodDelete, path+query, strings.NewReader(body))
		if trusted {
			req = permissionOperatorRequest(req)
		}
		req.Header.Set("Content-Type", "application/json")
		if revision != "" {
			req.Header.Set("If-Match", revision)
		}
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	preview := send("?dry_run=true", "", "", "{}", true)
	var impact accessapp.CredentialRevocationPreview
	if preview.Code != 200 || json.Unmarshal(preview.Body.Bytes(), &impact) != nil || impact.Principal != created.Access.ID || impact.Credential != created.Credential || impact.Revision == "" || impact.ApprovalClass != "trusted_confirmation" || preview.Header().Get("ETag") != `"`+impact.Revision+`"` || preview.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("preview lost server impact", preview.Code, preview.Body.String())
	}
	revision := preview.Header().Get("ETag")
	for _, probe := range []struct {
		query, revision, key, body string
		trusted                    bool
		want                       int
	}{
		{"", revision, "retry", "{}", false, 403},
		{"", "", "retry", "{}", true, 428},
		{"", `W/"weak"`, "retry", "{}", true, 400},
		{"", "*", "retry", "{}", true, 400},
		{"", `"one","two"`, "retry", "{}", true, 400},
		{"", `"stale"`, "retry", "{}", true, 412},
		{"", revision, "", "{}", true, 400},
		{"", revision, "two keys", "{}", true, 400},
		{"?dry_run=invalid", revision, "retry", "{}", true, 400},
		{"?unknown=true", revision, "retry", "{}", true, 400},
		{"", revision, "retry", "{}{}", true, 400},
		{"", revision, "retry", "[]", true, 400},
	} {
		response := send(probe.query, probe.revision, probe.key, probe.body, probe.trusted)
		if response.Code != probe.want {
			t.Fatalf("rejection=%d want=%d body=%s", response.Code, probe.want, response.Body.String())
		}
		current, err := handler.service.GetPrincipal(t.Context(), created.Access.ID)
		if err != nil || !reflect.DeepEqual(current, before) {
			t.Fatal("rejection changed Principal")
		}
		credentials, err := handler.service.ListCredentials(t.Context(), created.Access.ID)
		if err != nil || len(credentials) != 2 {
			t.Fatal("preview/rejection removed credentials")
		}
	}
	first := send("", revision, "retry", "{}", true)
	var applied accessapp.CredentialRevocation
	if first.Code != 200 || json.Unmarshal(first.Body.Bytes(), &applied) != nil || !applied.Revoked || applied.Replayed || applied.Credential != created.Credential.ID || !reflect.DeepEqual(applied.Principal.Permissions, before.Permissions) || first.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("apply acknowledgement invalid", first.Code, first.Body.String())
	}
	credentials, err := handler.service.ListCredentials(t.Context(), created.Access.ID)
	if err != nil || len(credentials) != 1 || credentials[0] != sibling.Credential {
		t.Fatal("apply removed sibling or retained selected credential")
	}
	replay := send("", revision, "retry", "{}", true)
	var recovered accessapp.CredentialRevocation
	if replay.Code != 200 || json.Unmarshal(replay.Body.Bytes(), &recovered) != nil || !recovered.Replayed || !recovered.Revoked || recovered.Credential != created.Credential.ID || !reflect.DeepEqual(recovered.Principal, applied.Principal) {
		t.Fatal("exact replay lost current state", replay.Code, replay.Body.String())
	}
	if response := send("", `"changed"`, "retry", "{}", true); response.Code != 409 || !strings.Contains(response.Body.String(), "idempotency_conflict") {
		t.Fatal("changed intent did not conflict")
	}
	if response := send("", revision, "new-key", "{}", true); response.Code != 412 {
		t.Fatal("fresh key reused stale consent")
	}
	if response := send("", revision, "retry", "{}", false); response.Code != 403 {
		t.Fatal("receipt bypassed admission")
	}
	// Initial ingress succeeds; authority disappears at the locked replay check.
	checks := 0
	req := httptest.NewRequest(http.MethodDelete, path, strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("If-Match", revision)
	req.Header.Set("Idempotency-Key", "retry")
	req = req.WithContext(contextWithAuthority(req, admittedAuthority(func(routing.WorkspaceSlug) (accessidentity.ID, error) {
		checks++
		if checks >= 2 {
			return accessidentity.ID{}, errors.New("withdrawn at transaction")
		}
		return accessidentity.Local, nil
	})))
	denied := httptest.NewRecorder()
	handler.ServeHTTP(denied, req)
	if denied.Code != 403 || checks != 2 {
		t.Fatal("replay skipped transaction-time admission", denied.Code, checks)
	}
	if response := send("?dry_run=true", "", "", "{}", true); response.Code != 404 {
		t.Fatal("absent credential preview fabricated impact")
	}
}
