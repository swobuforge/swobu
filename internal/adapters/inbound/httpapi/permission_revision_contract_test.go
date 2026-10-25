package httpapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	operatorclient "github.com/swobuforge/swobu/internal/app/operator/client"
)

func TestPermissionHTTPUsesRelationshipConsentAndRejectsStoreRevision(t *testing.T) {
	handler := accessHandler(t)
	principal, err := handler.service.Create(t.Context(), "target", "prod")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewLocalAdmissionHandler(handler, nil, nil))
	defer server.Close()
	client := operatorclient.New(server.Client(), server.URL)
	preview, err := client.PreviewWorkspacePermission(t.Context(), "prod", principal.Access.ID, false, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	read, err := client.GetWorkspacePermission(t.Context(), "prod", principal.Access.ID)
	if err != nil || read.Revision != preview.Revision {
		t.Fatal("GET and preview disagree on relationship revision")
	}
	if _, err := handler.service.Create(t.Context(), "sibling", "prod"); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodDelete, server.URL+"/api/v1/workspaces/prod/permissions/"+principal.Access.ID, strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("If-Match", `"`+handler.service.Revision()+`"`)
	request.Header.Set("Idempotency-Key", "store-revision-denied")
	before := handler.service.Revision()
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusPreconditionFailed || handler.service.Revision() != before {
		t.Fatal("whole-store precondition accepted or rejection changed authority")
	}
	previewRequest := request.Clone(t.Context())
	previewRequest.URL.RawQuery = "dry_run=true"
	// A real JSON body is required on this advisory command too.
	previewRequest.Body = io.NopCloser(strings.NewReader(`{}`))
	previewRequest.ContentLength = 2
	response, err = server.Client().Do(previewRequest)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusPreconditionFailed || handler.service.Revision() != before {
		t.Fatal("preview accepted whole-store revision or published authority")
	}
	reviewed, err := operatorclient.ReviewPermission(preview, "relationship-consent")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.ApplyWorkspacePermission(t.Context(), reviewed); err != nil {
		t.Fatalf("sibling edit invalidated reviewed permission: %v", err)
	}
	if err := handler.service.GrantPermission(t.Context(), principal.Access.ID, "prod"); err != nil {
		t.Fatal(err)
	}
	stale, err := operatorclient.ReviewPermission(preview, "fresh-key-old-permission")
	if err != nil {
		t.Fatal(err)
	}
	before = handler.service.Revision()
	if _, err := client.ApplyWorkspacePermission(t.Context(), stale); err == nil {
		t.Fatal("revoke/regrant resurrected old HTTP consent")
	}
	if handler.service.Revision() != before {
		t.Fatal("stale permission changed authority")
	}
}
