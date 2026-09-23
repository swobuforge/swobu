package accessstate

import (
	"crypto/sha256"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/routing"
)

func testWorkspace(t *testing.T, raw string) routing.WorkspaceSlug {
	t.Helper()
	slug, err := routing.ParseWorkspaceSlug(raw)
	if err != nil {
		t.Fatal(err)
	}
	return slug
}

func TestCreatePersistsDigestAndAuthenticatesAfterRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "access.json")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	access, issued, err := store.Create(" CI ", testWorkspace(t, "production"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(access.ID.String(), "acc_") || !strings.HasPrefix(issued.Credential.ID.String(), "acred_") || !strings.HasPrefix(issued.Token, "swak_") {
		t.Fatalf("unexpected IDs: %#v %#v", access, issued)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), issued.Token) {
		t.Fatal("plaintext token persisted")
	}
	digest := sha256.Sum256([]byte(issued.Token))
	if !strings.Contains(string(raw), encodeDigest(digest)) {
		t.Fatal("credential digest not persisted")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	resolution, err := reopened.Authenticate(issued.Token)
	if err != nil {
		t.Fatal(err)
	}
	if resolution.AccessID != access.ID || resolution.Workspace.String() != "production" {
		t.Fatalf("resolution = %#v", resolution)
	}
}

func TestRotationRevocationAndWorkspaceReconciliation(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "access.json"))
	if err != nil {
		t.Fatal(err)
	}
	access, first, err := store.Create("backend", testWorkspace(t, "production"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.IssueCredential(access.ID, testWorkspace(t, "staging"))
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := store.Authenticate(second.Token); got.AccessID != access.ID {
		t.Fatalf("rotated identity = %v", got.AccessID)
	}
	if _, err := store.RevokeCredential(access.ID, first.Credential.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Authenticate(first.Token); err == nil {
		t.Fatal("revoked credential authenticated")
	}
	if _, err := store.Authenticate(second.Token); err != nil {
		t.Fatalf("sibling credential revoked: %v", err)
	}
	removed, err := store.RevokeWorkspace(testWorkspace(t, "staging"))
	if err != nil || !removed {
		t.Fatalf("RevokeWorkspace = %v, %v", removed, err)
	}
	if _, err := store.Authenticate(second.Token); err == nil {
		t.Fatal("workspace credential remained active")
	}
}

func TestRevokeAccessPreservesIdentityAndPreventsIssuance(t *testing.T) {
	store, _ := Open(filepath.Join(t.TempDir(), "access.json"))
	access, issued, err := store.Create("backend", testWorkspace(t, "production"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RevokeAccess(access.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Authenticate(issued.Token); err == nil {
		t.Fatal("revoked access authenticated")
	}
	if _, err := store.IssueCredential(access.ID, testWorkspace(t, "production")); err == nil {
		t.Fatal("issued for revoked access")
	}
	listed := store.List()
	if len(listed) != 1 || listed[0].RevokedAt == nil || len(listed[0].Credentials) != 0 {
		t.Fatalf("listed = %#v", listed)
	}
}

func TestReconcileWorkspacesRevokesOnlyMissingWorkspaceCredentials(t *testing.T) {
	store, _ := Open(filepath.Join(t.TempDir(), "access.json"))
	access, missing, err := store.Create("backend", testWorkspace(t, "removed"))
	if err != nil {
		t.Fatal(err)
	}
	retained, err := store.IssueCredential(access.ID, testWorkspace(t, "production"))
	if err != nil {
		t.Fatal(err)
	}
	workspace := testWorkspace(t, "production")
	route, _ := routing.ParseRouteName("chat")
	targetID, _ := routing.ParseTargetID("primary")
	model, _ := routing.ParseUpstreamModel("model")
	provider, _ := routing.ParseProvider("openai", func(string) bool { return true })
	protocol, _ := routing.ParseProtocol("responses", provider, func(routing.Provider, string) bool { return true })
	connection, err := routing.NewStandardConnection(provider, "", "")
	if err != nil {
		t.Fatal(err)
	}
	target, err := routing.NewTarget(targetID, model, protocol, connection)
	if err != nil {
		t.Fatal(err)
	}
	tier, _ := routing.NewTier([]routing.Target{target})
	configuredRoute, _ := routing.NewRoute(route, []routing.Tier{tier})
	configuredWorkspace, _ := routing.NewWorkspace(workspace, route, []routing.Route{configuredRoute})
	config, _ := routing.NewConfig([]routing.Workspace{configuredWorkspace})
	changed, err := store.ReconcileWorkspaces(config)
	if err != nil || !changed {
		t.Fatalf("ReconcileWorkspaces=%v,%v", changed, err)
	}
	if _, err := store.Authenticate(missing.Token); err == nil {
		t.Fatal("missing Workspace credential survived reconciliation")
	}
	if _, err := store.Authenticate(retained.Token); err != nil {
		t.Fatalf("existing Workspace credential revoked: %v", err)
	}
}

func TestOpenRejectsCorruptUnsupportedOrFutureShapedState(t *testing.T) {
	for name, raw := range map[string]string{
		"corrupt":     `{`,
		"unsupported": `{"schema_version":2,"access":[]}`,
		"unknown":     `{"schema_version":1,"access":[],"future_field":true}`,
		"trailing":    `{"schema_version":1,"access":[]} {}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "access.json")
			if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Open(path); err == nil {
				t.Fatal("Open unexpectedly succeeded")
			}
		})
	}
}

func encodeDigest(digest [32]byte) string { return base64.RawURLEncoding.EncodeToString(digest[:]) }
