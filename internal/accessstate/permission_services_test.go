package accessstate

import (
	"bytes"
	"errors"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/routing"
)

// No new token is minted when the same Workspace permission is narrowed. The
// already-issued credential must lose the excluded Service immediately.
func TestPermissionServiceScopeSurvivesRestartAndRevocation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "principals.json")
	store, err := openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	ws, _ := routing.ParseWorkspaceSlug("engineering")
	principal, issued, err := store.Create("build-agent", ws)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []routing.ServiceKind{routing.ServiceModels, routing.ServiceMCP, routing.ServiceSearch} {
		if _, err := store.ResolveCredentialForService(issued.Credential.ID, kind); err != nil {
			t.Fatalf("initial %s: %v", kind, err)
		}
	}
	revision, err := store.PermissionRevision(principal.ID, ws)
	if err != nil {
		t.Fatal(err)
	}
	scope := []routing.ServiceKind{routing.ServiceModels, routing.ServiceSearch}
	mutation, err := store.ApplyPermissionServicesChecked(t.Context(), principal.ID, ws, revision, "scope-change-1", true, func() error { return nil }, scope)
	if err != nil || mutation.Replayed || mutation.Permission == nil {
		t.Fatalf("first apply: %#v %v", mutation, err)
	}
	if _, err := store.ResolveCredentialForService(issued.Credential.ID, routing.ServiceMCP); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("MCP still authorized: %v", err)
	}
	if _, err := store.ResolveCredentialForService(issued.Credential.ID, routing.ServiceModels); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ResolveCredentialForService(issued.Credential.ID, routing.ServiceSearch); err != nil {
		t.Fatal(err)
	}
	// Retry acknowledges the same intent without reviving the discarded scope.
	replay, err := store.ApplyPermissionServicesChecked(t.Context(), principal.ID, ws, revision, "scope-change-1", true, func() error { return nil }, scope)
	if err != nil || !replay.Replayed {
		t.Fatalf("replay: %#v %v", replay, err)
	}
	if _, err := store.ApplyPermissionServicesChecked(t.Context(), principal.ID, ws, revision, "scope-change-1", true, func() error { return nil }, []routing.ServiceKind{routing.ServiceModels}); !errors.Is(err, ErrPermissionIdempotencyConflict) {
		t.Fatalf("conflicting replay: %v", err)
	}
	reopened, err := openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.ResolveCredentialForService(issued.Credential.ID, routing.ServiceMCP); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("MCP permitted after restart: %v", err)
	}
	if _, err := reopened.ResolveCredentialForService(issued.Credential.ID, routing.ServiceModels); err != nil {
		t.Fatal(err)
	}
	next, err := reopened.PermissionRevision(principal.ID, ws)
	if err != nil || next == revision {
		t.Fatalf("scope mutation did not change relation revision: %q %v", next, err)
	}
	_, err = reopened.ApplyPermissionServicesChecked(t.Context(), principal.ID, ws, revision, "new-key", true, func() error { return nil }, []routing.ServiceKind{routing.ServiceMCP})
	if !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale revision accepted: %v", err)
	}
	if err := reopened.RevokePermission(principal.ID, ws); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.ResolveCredentialForService(issued.Credential.ID, routing.ServiceModels); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("revoked grant authorized: %v", err)
	}
}

func TestPermissionScopeValidationAndCanonicalIdempotency(t *testing.T) {
	store, err := openTestStore(filepath.Join(t.TempDir(), "principals.json"))
	if err != nil {
		t.Fatal(err)
	}
	ws, _ := routing.ParseWorkspaceSlug("engineering")
	principal, _, err := store.Create("build", ws)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := store.PermissionRevision(principal.ID, ws)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]routing.ServiceKind{{}, {routing.ServiceModels, routing.ServiceModels}, {"invalid"}} {
		before := store.Revision()
		_, err := store.PreviewPermissionServices(principal.ID, ws, revision, true, time.Time{}, bad)
		if err == nil || store.Revision() != before {
			t.Fatalf("invalid preview did not fail closed: %#v %v", bad, err)
		}
		_, err = store.ApplyPermissionServicesChecked(t.Context(), principal.ID, ws, revision, "bad-key", true, func() error { return nil }, bad)
		if err == nil || store.Revision() != before {
			t.Fatalf("invalid write did not fail closed: %#v %v", bad, err)
		}
	}
	preview, err := store.PreviewPermissionServices(principal.ID, ws, revision, true, time.Time{}, []routing.ServiceKind{routing.ServiceSearch, routing.ServiceModels})
	if err != nil || preview.After == nil || len(preview.After.Services) != 2 || preview.After.Services[0] != routing.ServiceModels {
		t.Fatalf("noncanonical preview: %#v %v", preview, err)
	}
	if _, err := store.ApplyPermissionServicesChecked(t.Context(), principal.ID, ws, revision, "sorted-key", true, func() error { return nil }, []routing.ServiceKind{routing.ServiceSearch, routing.ServiceModels}); err != nil {
		t.Fatal(err)
	}
	replay, err := store.ApplyPermissionServicesChecked(t.Context(), principal.ID, ws, revision, "sorted-key", true, func() error { return nil }, []routing.ServiceKind{routing.ServiceModels, routing.ServiceSearch})
	if err != nil || !replay.Replayed {
		t.Fatalf("canonical replay: %#v %v", replay, err)
	}
}

func TestScopedPermissionInlineDocumentRoundTrip(t *testing.T) {
	var persisted []byte
	publisher := func(raw []byte) error {
		persisted = append([]byte(nil), raw...)
		return nil
	}
	store, err := OpenInline(nil, publisher)
	if err != nil {
		t.Fatal(err)
	}
	ws, _ := routing.ParseWorkspaceSlug("engineering")
	principal, issued, err := store.Create("remote-agent", ws)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := store.PermissionRevision(principal.ID, ws)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApplyPermissionServicesChecked(t.Context(), principal.ID, ws, revision, "scoped-inline", true, func() error { return nil }, []routing.ServiceKind{routing.ServiceMCP}); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateDocument(persisted); err != nil {
		t.Fatalf("inline Node Principal document was invalid: %v", err)
	}
	for _, poison := range []string{`"services":[]`, `"services":["unknown"]`, `"services":["mcp","mcp"]`} {
		broken := regexp.MustCompile(`"services":\s*\[\s*"mcp"\s*\]`).ReplaceAll(persisted, []byte(poison))
		if bytes.Equal(broken, persisted) {
			t.Fatal("fixture lost the restricted Service scope")
		}
		if _, err := ValidateDocument(broken); err == nil {
			t.Fatalf("invalid persisted scope accepted: %s", poison)
		}
	}
	reopened, err := OpenInline(persisted, publisher)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.ResolveCredentialForService(issued.Credential.ID, routing.ServiceModels); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("restarted Node broadened to Models: %v", err)
	}
	if _, err := reopened.ResolveCredentialForService(issued.Credential.ID, routing.ServiceMCP); err != nil {
		t.Fatalf("restarted Node lost MCP: %v", err)
	}
}
