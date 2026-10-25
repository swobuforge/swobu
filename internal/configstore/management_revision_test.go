package configstore

import (
	"bytes"
	"errors"
	"os"
	"testing"

	"github.com/swobuforge/swobu/internal/routing"
)

func TestManagementRevisionPersistsAndOnlyCommittedSemanticChangesAdvance(t *testing.T) {
	store, path := openTestStore(t)
	if _, err := store.Update(t.Context(), func(c routing.Config) (routing.Config, error) {
		created, err := addWorkspace(t, "dev")(c)
		if err != nil {
			return routing.Config{}, err
		}
		slug, _ := routing.ParseWorkspaceSlug("dev")
		return created.SetServiceEnabled(slug, routing.ServiceSearch, true)
	}); err != nil {
		t.Fatal(err)
	}
	slug, _ := routing.ParseWorkspaceSlug("dev")
	original := store.Config()
	check := func(want uint64) {
		t.Helper()
		workspace, err := store.GetWorkspace(t.Context(), slug)
		if err != nil || workspace.ManagementRevision() != want {
			t.Fatalf("revision=%d want=%d err=%v", workspace.ManagementRevision(), want, err)
		}
	}
	check(1)
	// Caller-supplied metadata alone is not a semantic edit or permission to
	// rewrite the daemon's revision authority.
	if _, err := store.Update(t.Context(), func(c routing.Config) (routing.Config, error) {
		workspace, _ := c.Workspace(slug)
		return routing.NewConfig([]routing.Workspace{workspace.WithManagementRevision(9000)})
	}); err != nil {
		t.Fatal(err)
	}
	check(1)
	if _, err := store.Update(t.Context(), addWorkspace(t, "other")); err != nil {
		t.Fatal(err)
	}
	check(1)
	if _, err := store.Update(t.Context(), func(c routing.Config) (routing.Config, error) { return c, nil }); err != nil {
		t.Fatal(err)
	}
	check(1)
	edit := func(c routing.Config) (routing.Config, error) { return c.SetSearchRoute(slug, "search", nil) }
	before, _ := os.ReadFile(path)
	rename := store.ops.rename
	store.ops.rename = func(string, string) error { return errors.New("injected failure") }
	if _, err := store.Update(t.Context(), edit); err == nil {
		t.Fatal("failed write succeeded")
	}
	check(1)
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("failed write changed bytes")
	}
	store.ops.rename = rename
	if _, err := store.Update(t.Context(), edit); err != nil {
		t.Fatal(err)
	}
	check(2)
	if _, err := store.Update(t.Context(), func(routing.Config) (routing.Config, error) { return original, nil }); err != nil {
		t.Fatal(err)
	}
	check(3)
	if _, err := store.Update(t.Context(), func(c routing.Config) (routing.Config, error) { return c.SetWorkspaceDisplayName(slug, "Research") }); err != nil {
		t.Fatal(err)
	}
	check(4)
	publication, _ := store.GetWorkspace(t.Context(), slug)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	workspace, err := reopened.GetWorkspace(t.Context(), slug)
	if err != nil || workspace.ManagementRevision() != 4 || workspace.ConfigurationRevision() == publication.ConfigurationRevision() {
		t.Fatal("reopen mixed durable and ephemeral revision authority")
	}
}

func TestManagementRevisionGrammarAndOverflow(t *testing.T) {
	for _, value := range []string{"", "    management_revision: 0\n", "    management_revision: 12\n"} {
		raw := []byte("schema_version: 5\nworkspaces:\n  dev:\n" + value + "    routes: []\n")
		config, err := decode(raw)
		if value == "    management_revision: 0\n" {
			if err == nil {
				t.Fatal("zero revision accepted")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		want := uint64(1)
		if value != "" {
			want = 12
		}
		if config.Workspaces()[0].ManagementRevision() != want {
			t.Fatal("revision decode")
		}
	}
	store, _ := openTestStore(t)
	slug, _ := routing.ParseWorkspaceSlug("dev")
	workspace, err := routing.RestoreWorkspaceWithSearchConnection(slug, nil, nil, nil, "")
	workspace, err = workspace.SetServiceEnabled(routing.ServiceSearch, true)
	if err != nil {
		t.Fatal(err)
	}
	workspace = workspace.WithManagementRevision(^uint64(0)).WithConfigurationRevision("test")
	config, _ := routing.NewConfig([]routing.Workspace{workspace})
	store.snapshot.Store(&config)
	if _, err := store.Update(t.Context(), func(c routing.Config) (routing.Config, error) {
		return c.SetSearchRoute(workspace.Slug(), "search", nil)
	}); err == nil {
		t.Fatal("revision overflow accepted")
	}
}
