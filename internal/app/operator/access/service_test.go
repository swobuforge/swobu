package access

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/accessstate"
	"github.com/swobuforge/swobu/internal/app/operator/routebindings"
	"github.com/swobuforge/swobu/internal/configstore"
	"github.com/swobuforge/swobu/internal/nodefile"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestListPrincipalsDerivesDistinctWorkspacePermissionsWithoutReissuingCredentials(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "swobu.yaml"), []byte("schema_version: 5\nworkspaces: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	configStore, err := configstore.Open(filepath.Join(dir, "swobu.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	defer configStore.Close()
	node, err := nodefile.Open(filepath.Join(dir, "swobu.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	accessStore, err := accessstate.OpenInline(nil, node.PublishPrincipals)
	if err != nil {
		t.Fatal(err)
	}
	binding := &routebindings.Coordinator{}
	service, err := NewService(accessStore, configStore, binding)
	if err != nil {
		t.Fatal(err)
	}
	coding := mustWorkspace(t, "coding")
	research := mustWorkspace(t, "research")
	codingSlug, _ := routing.ParseWorkspaceSlug(coding)
	researchSlug, _ := routing.ParseWorkspaceSlug(research)
	identity, issued, err := accessStore.Create("Alice", codingSlug)
	if err != nil {
		t.Fatal(err)
	}
	if err := accessStore.GrantPermission(identity.ID, researchSlug); err != nil {
		t.Fatal(err)
	}
	if _, err := accessStore.IssueCredential(identity.ID, researchSlug); err != nil {
		t.Fatal(err)
	}
	principals := service.ListPrincipals(context.Background())
	if len(principals) != 1 || len(principals[0].Permissions) != 2 {
		t.Fatalf("principals = %#v", principals)
	}
	if issued.Token == "" {
		t.Fatal("initial credential was not issued")
	}
	if got := len(accessStore.List()[0].Credentials); got != 2 {
		t.Fatalf("credential count = %d, want two stable credentials", got)
	}
}

func TestPermissionGrantReceiptLivesOnlyInNodeAuthority(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "swobu.yaml")
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("schema_version: 5\nworkspaces: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := configstore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer config.Close()
	ws := mustSlug(t, "prod")
	if _, err := config.Update(context.Background(), func(c routing.Config) (routing.Config, error) {
		return c.CreateEmptyWorkspace(routing.EmptyWorkspaceSeed{Slug: ws})
	}); err != nil {
		t.Fatal(err)
	}
	node, err := nodefile.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	state, err := accessstate.OpenInline(nil, node.PublishPrincipals)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(state, config, &routebindings.Coordinator{})
	if err != nil {
		t.Fatal(err)
	}
	principal, issued, err := state.Create("Alice", ws)
	if err != nil {
		t.Fatal(err)
	}
	const key = "receipt-local-only"
	if err := service.GrantPermissionIdempotent(principal.ID.String(), "missing", "", key); !errors.Is(err, routing.ErrNotFound) {
		t.Fatalf("missing Workspace admitted: %v", err)
	}
	if err := service.GrantPermissionIdempotent(principal.ID.String(), "prod", "", key); err != nil {
		t.Fatal(err)
	}
	raw, err := node.PrincipalDocument()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), key) || strings.Contains(string(raw), issued.Token) || !strings.Contains(string(raw), "permission_receipts") {
		t.Fatal("inline authority lost receipt or leaked secret")
	}
	if _, err := os.Stat(filepath.Join(dir, "access.json")); !os.IsNotExist(err) {
		t.Fatal("created obsolete Principal sidecar", err)
	}
	reopened, err := accessstate.OpenInline(raw, node.PublishPrincipals)
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := NewService(reopened, config, &routebindings.Coordinator{})
	if err != nil {
		t.Fatal(err)
	}
	revision := reopened.Revision()
	if err := resumed.GrantPermissionIdempotent(principal.ID.String(), "prod", "", key); err != nil {
		t.Fatal(err)
	}
	if reopened.Revision() != revision {
		t.Fatal("replay mutated authoritative grant")
	}
}

func mustWorkspace(t *testing.T, raw string) string {
	t.Helper()
	value, err := routing.ParseWorkspaceSlug(raw)
	if err != nil {
		t.Fatal(err)
	}
	return value.String()
}

func mustSlug(t *testing.T, raw string) routing.WorkspaceSlug {
	t.Helper()
	slug, err := routing.ParseWorkspaceSlug(raw)
	if err != nil {
		t.Fatal(err)
	}
	return slug
}
