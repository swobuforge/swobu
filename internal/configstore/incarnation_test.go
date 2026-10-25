package configstore

import (
	"testing"

	"github.com/swobuforge/swobu/internal/routing"
)

func TestWorkspaceIncarnationSurvivesRestartButNotDeleteRecreate(t *testing.T) {
	store, path := openTestStore(t)
	if _, err := store.Update(t.Context(), addWorkspace(t, "dev")); err != nil {
		t.Fatal(err)
	}
	slug, _ := routing.ParseWorkspaceSlug("dev")
	original := store.Config()
	first, _ := original.Workspace(slug)
	if len(first.ManagementIncarnation()) != 64 {
		t.Fatal("missing incarnation")
	}
	if _, err := store.Update(t.Context(), func(c routing.Config) (routing.Config, error) {
		workspace, _ := c.Workspace(slug)
		return routing.NewConfig([]routing.Workspace{workspace.WithManagementIncarnation("spoofed")})
	}); err != nil {
		t.Fatal(err)
	}
	unspoofed, _ := store.GetWorkspace(t.Context(), slug)
	if unspoofed.ManagementIncarnation() != first.ManagementIncarnation() || unspoofed.ManagementRevision() != first.ManagementRevision() {
		t.Fatal("caller metadata replaced lifetime authority")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	current, _ := reopened.GetWorkspace(t.Context(), slug)
	if current.ManagementIncarnation() != first.ManagementIncarnation() {
		t.Fatal("restart changed incarnation")
	}
	if _, err := reopened.Update(t.Context(), func(c routing.Config) (routing.Config, error) { return c.DeleteWorkspace(slug) }); err != nil {
		t.Fatal(err)
	}
	// Even restoring an old in-process snapshot must create new lifetime
	// authority, not accept the snapshot's stale identity.
	if _, err := reopened.Update(t.Context(), func(routing.Config) (routing.Config, error) { return original, nil }); err != nil {
		t.Fatal(err)
	}
	recreated, _ := reopened.GetWorkspace(t.Context(), slug)
	if recreated.ManagementRevision() != first.ManagementRevision() || recreated.ManagementIncarnation() == first.ManagementIncarnation() {
		t.Fatal("delete/recreate reused lifetime authority")
	}
	if _, err := reopened.Update(t.Context(), func(c routing.Config) (routing.Config, error) {
		return c.SetWorkspaceDisplayName(slug, "Renamed display")
	}); err != nil {
		t.Fatal(err)
	}
	updated, _ := reopened.GetWorkspace(t.Context(), slug)
	if updated.ManagementIncarnation() != recreated.ManagementIncarnation() || updated.Slug() != slug {
		t.Fatal("display edit changed lifetime/address identity")
	}
}

func TestLegacyWorkspaceIncarnationIsDeterministicAndMalformedIdentityRejects(t *testing.T) {
	raw := []byte("schema_version: 5\nworkspaces:\n  dev:\n    routes: []\n")
	first, err := decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	second, err := decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if first.Workspaces()[0].ManagementIncarnation() != second.Workspaces()[0].ManagementIncarnation() {
		t.Fatal("legacy read rotated identity")
	}
	encoded, err := encodeDocument(first, routing.ReferenceSchemaVersion)
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Workspaces()[0].ManagementIncarnation() != first.Workspaces()[0].ManagementIncarnation() {
		t.Fatal("first persistence rotated legacy identity")
	}
	if _, err := decode([]byte("schema_version: 5\nworkspaces:\n  dev:\n    management_incarnation: invalid\n    routes: []\n")); err == nil {
		t.Fatal("malformed incarnation accepted")
	}
}

func TestMigrationPreservesLegacyWorkspaceIncarnation(t *testing.T) {
	raw := []byte("schema_version: 5\nworkspaces:\n  dev:\n    routes: []\n")
	before, err := decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	migrated, err := reencodeCurrentDocument(raw)
	if err != nil {
		t.Fatal(err)
	}
	after, err := decode(migrated)
	if err != nil {
		t.Fatal(err)
	}
	if before.Workspaces()[0].ManagementIncarnation() != after.Workspaces()[0].ManagementIncarnation() {
		t.Fatal("schema migration changed existing lifetime identity")
	}
}

func TestReplacementSlugCannotInheritRemovedWorkspaceLifetime(t *testing.T) {
	store, _ := openTestStore(t)
	if _, err := store.Update(t.Context(), addWorkspace(t, "dev")); err != nil {
		t.Fatal(err)
	}
	slug, _ := routing.ParseWorkspaceSlug("dev")
	before, err := store.GetWorkspace(t.Context(), slug)
	if err != nil {
		t.Fatal(err)
	}
	replacementSlug, _ := routing.ParseWorkspaceSlug("replacement")
	if _, err := store.Update(t.Context(), func(routing.Config) (routing.Config, error) {
		replacement, err := routing.RestoreWorkspace(replacementSlug, nil, nil)
		if err != nil {
			return routing.Config{}, err
		}
		// Caller-carried source stamps cannot turn another address into the
		// original lifetime, even in a single owner transaction.
		replacement = replacement.WithConfigurationRevision(before.ConfigurationRevision()).WithManagementIncarnation(before.ManagementIncarnation()).WithManagementRevision(before.ManagementRevision())
		return routing.NewConfig([]routing.Workspace{replacement})
	}); err != nil {
		t.Fatal(err)
	}
	after, err := store.GetWorkspace(t.Context(), replacementSlug)
	if err != nil || after.ManagementIncarnation() == before.ManagementIncarnation() || after.ManagementRevision() != 1 {
		t.Fatal("replacement address inherited removed lifetime")
	}
}
