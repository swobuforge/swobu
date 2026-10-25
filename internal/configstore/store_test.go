package configstore

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/routing"
)

func openTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "swobu.yaml")
	if err := os.WriteFile(path, []byte("schema_version: 5\nworkspaces: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store, path
}

func TestOpenOrCreateOwnsInitialDocumentAndLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "swobu.yaml")
	store, err := OpenOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "schema_version: 5\nworkspaces: {}\n" {
		t.Fatalf("initial document = %q", raw)
	}
	if store.Config().WorkspaceCount() != 0 {
		t.Fatalf("workspace count = %d", store.Config().WorkspaceCount())
	}
	if _, err := OpenOrCreate(path); !errors.Is(err, ErrLocked) {
		t.Fatalf("second OpenOrCreate error = %v, want ErrLocked", err)
	}
}

func TestOpenRejectsSymlinkWithoutChangingTarget(t *testing.T) {
	for _, suffix := range []string{"", ".lock"} {
		t.Run("config"+suffix, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Chmod(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "swobu.yaml")
			target := filepath.Join(t.TempDir(), "external.yaml")
			content := []byte("schema_version: 5\nworkspaces: {}\n")
			if err := os.WriteFile(target, content, 0o644); err != nil {
				t.Fatal(err)
			}

			if err := os.Symlink(target, path+suffix); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}
			store, err := OpenOrCreate(path)
			if store != nil {
				_ = store.Close()
			}
			if err == nil {
				t.Fatal("symlink admitted")
			}
			after, err := os.ReadFile(target)
			if err != nil || !bytes.Equal(content, after) {
				t.Fatalf("symlink target changed: %v", err)
			}
		})
	}
}

func storeTarget(t *testing.T, id string) routing.Target {
	t.Helper()
	targetID, _ := routing.ParseTargetID(id)
	model, _ := routing.ParseUpstreamModel("model-" + id)
	provider, _ := routing.ParseProvider("openai", func(raw string) bool { return raw == "openai" })
	connection, _ := connectionidentity.Parse("cn_shared")
	protocol, _ := routing.ParseProtocol("responses", provider, func(routing.Provider, string) bool { return true })
	target, err := routing.NewTarget(targetID, model, protocol, connection)
	if err != nil {
		t.Fatal(err)
	}
	return target
}

func addWorkspace(t *testing.T, rawSlug string) func(routing.Config) (routing.Config, error) {
	t.Helper()
	return func(config routing.Config) (routing.Config, error) {
		slug, _ := routing.ParseWorkspaceSlug(rawSlug)
		route, _ := routing.ParseRouteName("chat")
		return config.CreateWorkspace(routing.WorkspaceSeed{Slug: slug, Route: route, Target: storeTarget(t, rawSlug)})
	}
}

func TestStorePublishesOnlyAfterSuccessfulRename(t *testing.T) {
	store, path := openTestStore(t)
	before, _ := os.ReadFile(path)
	store.ops.rename = func(string, string) error { return errors.New("injected rename failure") }
	if _, err := store.Update(context.Background(), addWorkspace(t, "dev")); err == nil {
		t.Fatal("Update unexpectedly succeeded")
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(before) || store.Config().WorkspaceCount() != 0 {
		t.Fatal("disk or memory changed after failed rename")
	}
}

func TestStoreRejectsInvalidMCPAliasesWithoutPublication(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "swobu.yaml")
	if err := os.WriteFile(path, []byte("schema_version: 5\nworkspaces: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if _, err := store.Update(t.Context(), func(c routing.Config) (routing.Config, error) {
		created, err := addWorkspace(t, "dev")(c)
		if err != nil {
			return routing.Config{}, err
		}
		slug, _ := routing.ParseWorkspaceSlug("dev")
		return created.SetServiceEnabled(slug, routing.ServiceMCP, true)
	}); err != nil {
		t.Fatal(err)
	}
	slug, _ := routing.ParseWorkspaceSlug("dev")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	original, _ := store.Config().Workspace(slug)
	for _, alias := range []string{"", "swobu", "Docs", "1docs", "docs\t", "docs:extra", "é", strings.Repeat("a", 33)} {
		t.Run(alias, func(t *testing.T) {
			_, err := store.Update(t.Context(), func(current routing.Config) (routing.Config, error) {
				return current.SetMCPMount(slug, alias, routing.MCPMount{ConnectionID: "docs", AllowedTools: []string{"search"}})
			})
			if !errors.Is(err, routing.ErrInvalidConfig) {
				t.Fatalf("invalid alias error = %v", err)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			workspace, _ := store.Config().Workspace(slug)
			if !bytes.Equal(before, after) || len(workspace.MCPMounts()) != 0 || workspace.ConfigurationRevision() != original.ConfigurationRevision() || workspace.ManagementRevision() != original.ManagementRevision() {
				t.Fatal("invalid alias changed durable or published Workspace state")
			}
		})
	}
	alias := "a" + strings.Repeat("b", 30) + "2"
	if _, err := store.Update(t.Context(), func(current routing.Config) (routing.Config, error) {
		return current.SetMCPMount(slug, alias, routing.MCPMount{ConnectionID: "docs", AllowedTools: []string{"search"}})
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	workspace, _ := reopened.Config().Workspace(slug)
	if mounts := workspace.MCPMounts(); len(mounts) != 1 || mounts[alias].ConnectionID != "docs" {
		t.Fatalf("valid boundary alias changed on reopen: %#v", mounts)
	}
}

func TestWorkspaceConfigurationRevisionTracksSuccessfulSemanticPublication(t *testing.T) {
	store, path := openTestStore(t)
	if _, err := store.Update(t.Context(), func(c routing.Config) (routing.Config, error) {
		created, err := addWorkspace(t, "dev")(c)
		if err != nil {
			return routing.Config{}, err
		}
		slug, _ := routing.ParseWorkspaceSlug("dev")
		return created.SetServiceEnabled(slug, routing.ServiceMCP, true)
	}); err != nil {
		t.Fatal(err)
	}
	slug, _ := routing.ParseWorkspaceSlug("dev")
	original := store.Config()
	workspace, _ := original.Workspace(slug)
	revision := workspace.ConfigurationRevision()
	if revision == "" {
		t.Fatal("published Workspace has no revision")
	}
	check := func(expected string) {
		t.Helper()
		workspace, err := store.GetWorkspace(t.Context(), slug)
		if err != nil || workspace.ConfigurationRevision() != expected {
			t.Fatalf("revision=%q expected=%q err=%v", workspace.ConfigurationRevision(), expected, err)
		}
	}
	if _, err := store.Update(t.Context(), addWorkspace(t, "other")); err != nil {
		t.Fatal(err)
	}
	check(revision)
	edit := func(current routing.Config) (routing.Config, error) {
		return current.SetMCPMount(slug, "docs", routing.MCPMount{ConnectionID: "docs", AllowedTools: []string{"search"}})
	}
	rename := store.ops.rename
	store.ops.rename = func(string, string) error { return errors.New("injected rename failure") }
	if _, err := store.Update(t.Context(), edit); err == nil {
		t.Fatal("failed publication succeeded")
	}
	check(revision)
	store.ops.rename = rename
	if _, err := store.Update(t.Context(), edit); err != nil {
		t.Fatal(err)
	}
	changed, _ := store.GetWorkspace(t.Context(), slug)
	if changed.ConfigurationRevision() == revision {
		t.Fatal("semantic commit retained revision")
	}
	if _, err := store.Update(t.Context(), func(routing.Config) (routing.Config, error) { return original, nil }); err != nil {
		t.Fatal(err)
	}
	restored, _ := store.GetWorkspace(t.Context(), slug)
	if restored.ConfigurationRevision() == revision || restored.ConfigurationRevision() == changed.ConfigurationRevision() {
		t.Fatal("restoring settings revived an old revision")
	}
	if _, err := store.Update(t.Context(), func(current routing.Config) (routing.Config, error) { return current, nil }); err != nil {
		t.Fatal(err)
	}
	check(restored.ConfigurationRevision())
	if _, err := store.Update(t.Context(), func(current routing.Config) (routing.Config, error) { return current.DeleteWorkspace(slug) }); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Update(t.Context(), func(routing.Config) (routing.Config, error) { return original, nil }); err != nil {
		t.Fatal(err)
	}
	recreated, _ := store.GetWorkspace(t.Context(), slug)
	if recreated.ConfigurationRevision() == revision || recreated.ConfigurationRevision() == restored.ConfigurationRevision() {
		t.Fatal("delete/recreate revived an old revision")
	}
	raw, err := os.ReadFile(path)
	if err != nil || bytes.Contains(raw, []byte(recreated.ConfigurationRevision())) {
		t.Fatal("runtime revision leaked into authored YAML")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	fresh, _ := reopened.GetWorkspace(t.Context(), slug)
	if fresh.ConfigurationRevision() == "" || fresh.ConfigurationRevision() == recreated.ConfigurationRevision() {
		t.Fatal("reopened store reused runtime revision")
	}
}

func TestRecoveryCannotBypassActiveStoreLock(t *testing.T) {
	_, path := openTestStore(t)
	calls := 0
	if _, err := OpenOrCreateAfterRecovery(path, func() error { calls++; return nil }); !errors.Is(err, ErrLocked) {
		t.Fatal("recovery bypassed lifetime lock", err)
	}
	if calls != 0 {
		t.Fatal("recovery effects ran before lock")
	}
}

func TestStorePreservesReferenceSchemaOnUpdate(t *testing.T) {
	store, path := openTestStore(t)
	if _, err := store.Update(context.Background(), addWorkspace(t, "dev")); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if !bytes.HasPrefix(raw, []byte("schema_version: 5\n")) {
		t.Fatal("current publication changed schema")
	}
}

func TestFailedUpdateTargetPersistenceLeavesSnapshotAndDiskUnchanged(t *testing.T) {
	store, path := openTestStore(t)
	if _, err := store.Update(context.Background(), addWorkspace(t, "dev")); err != nil {
		t.Fatal(err)
	}
	beforeDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	beforeModel := storedTargetModel(t, store.Config(), "dev", "chat", "dev")
	store.ops.rename = func(string, string) error { return errors.New("injected rename failure") }
	replacement := storeTarget(t, "replacement")

	_, err = store.Update(context.Background(), func(config routing.Config) (routing.Config, error) {
		slug, _ := routing.ParseWorkspaceSlug("dev")
		route, _ := routing.ParseRouteName("chat")
		workspace, _ := config.Workspace(slug)
		current, _ := workspace.Route(route)
		spec := current.Spec()
		spec.Tiers[0].Targets[0].Settings = routing.TargetSettings{Model: replacement.Model(), Protocol: replacement.Protocol(), ConnectionID: replacement.ConnectionID()}
		return config.ApplyRouteSpec(slug, route, spec)
	})
	if err == nil {
		t.Fatal("Update unexpectedly succeeded")
	}

	if got := storedTargetModel(t, store.Config(), "dev", "chat", "dev"); got != beforeModel {
		t.Fatalf("published model = %q after failed persistence, want %q", got, beforeModel)
	}
	afterDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(afterDisk) != string(beforeDisk) {
		t.Fatal("disk changed after failed route replacement persistence")
	}
}

func TestTargetVersionSurvivesStoreRestartAndAdvancesOnce(t *testing.T) {
	store, path := openTestStore(t)
	if _, err := store.Update(context.Background(), addWorkspace(t, "dev")); err != nil {
		t.Fatal(err)
	}
	slug, _ := routing.ParseWorkspaceSlug("dev")
	name, _ := routing.ParseRouteName("chat")
	if _, err := store.Update(context.Background(), func(config routing.Config) (routing.Config, error) {
		workspace, _ := config.Workspace(slug)
		route, _ := workspace.Route(name)
		spec := route.Spec()
		spec.Tiers[0].Targets[0].Settings.Model, _ = routing.ParseUpstreamModel("changed")
		return config.ApplyRouteSpec(slug, name, spec)
	}); err != nil {
		t.Fatal(err)
	}
	before := storedTargetVersion(t, store.Config(), "dev", "chat", "dev")
	if before != 2 {
		t.Fatalf("version before restart = %d, want 2", before)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if after := storedTargetVersion(t, reopened.Config(), "dev", "chat", "dev"); after != before {
		t.Fatalf("version after restart = %d, want %d", after, before)
	}
}

func storedTargetVersion(t *testing.T, config routing.Config, rawSlug, rawRoute, rawID string) routing.TargetVersion {
	t.Helper()
	slug, _ := routing.ParseWorkspaceSlug(rawSlug)
	routeName, _ := routing.ParseRouteName(rawRoute)
	id, _ := routing.ParseTargetID(rawID)
	workspace, _ := config.Workspace(slug)
	route, _ := workspace.Route(routeName)
	for _, tier := range route.Tiers() {
		for _, target := range tier.Targets() {
			if target.ID() == id {
				return target.Version()
			}
		}
	}
	t.Fatalf("target %q missing", rawID)
	return 0
}

func storedTargetModel(t *testing.T, config routing.Config, rawSlug, rawRoute, rawID string) string {
	t.Helper()
	slug, _ := routing.ParseWorkspaceSlug(rawSlug)
	routeName, _ := routing.ParseRouteName(rawRoute)
	id, _ := routing.ParseTargetID(rawID)
	workspace, ok := config.Workspace(slug)
	if !ok {
		t.Fatalf("workspace %q missing", rawSlug)
	}
	route, ok := workspace.Route(routeName)
	if !ok {
		t.Fatalf("route %q missing", rawRoute)
	}
	for _, tier := range route.Tiers() {
		for _, target := range tier.Targets() {
			if target.ID() == id {
				return target.Model().String()
			}
		}
	}
	t.Fatalf("target %q missing", rawID)
	return ""
}

func TestStoreRejectsShortWriteAndRetainsOldState(t *testing.T) {
	store, path := openTestStore(t)
	before, _ := os.ReadFile(path)
	store.ops.write = func(file *os.File, raw []byte) (int, error) { return file.Write(raw[:len(raw)/2]) }
	if _, err := store.Update(context.Background(), addWorkspace(t, "dev")); err == nil {
		t.Fatal("Update unexpectedly succeeded")
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(before) || store.Config().WorkspaceCount() != 0 {
		t.Fatal("state changed after short write")
	}
}

func TestStoreSerializesConcurrentCommandsWithoutLostUpdate(t *testing.T) {
	store, _ := openTestStore(t)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, slug := range []string{"one", "two"} {
		slug := slug
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.Update(context.Background(), addWorkspace(t, slug))
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if store.Config().WorkspaceCount() != 2 {
		t.Fatalf("workspace count = %d", store.Config().WorkspaceCount())
	}
}

func TestStoreHoldsExclusivePathLockForLifetime(t *testing.T) {
	store, path := openTestStore(t)
	if _, err := Open(path); !errors.Is(err, ErrLocked) {
		t.Fatalf("second Open error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = reopened.Close()
}

func TestClosedStoreRejectsUpdate(t *testing.T) {
	store, path := openTestStore(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Update(context.Background(), addWorkspace(t, "dev")); !errors.Is(err, ErrStoreClosed) {
		t.Fatalf("Update error = %v, want ErrStoreClosed", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("closed store changed disk state")
	}
}

func TestDirectorySyncFailureCommitsAndStoreRemainsWritable(t *testing.T) {
	store, path := openTestStore(t)
	var logs bytes.Buffer
	store.logger = slog.New(slog.NewTextHandler(&logs, nil))
	renameCount := 0
	realRename := store.ops.rename
	store.ops.rename = func(oldPath, newPath string) error {
		renameCount++
		return realRename(oldPath, newPath)
	}
	store.ops.syncDir = func(string) error { return errors.New("injected directory sync failure") }

	committed, err := store.Update(context.Background(), addWorkspace(t, "dev"))
	if err != nil {
		t.Fatalf("Update error = %v, want committed success", err)
	}
	if committed.WorkspaceCount() != 1 {
		t.Fatalf("committed workspace count = %d, want 1", committed.WorkspaceCount())
	}
	if !strings.Contains(logs.String(), "routing config directory sync failed after commit") {
		t.Fatalf("warning log missing: %q", logs.String())
	}
	if renameCount != 1 {
		t.Fatalf("rename count = %d, want one logical commit and no rollback", renameCount)
	}
	if store.Config().WorkspaceCount() != 1 {
		t.Fatal("committed snapshot was not published after rename")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.WorkspaceCount() != 1 {
		t.Fatal("renamed file does not contain committed snapshot")
	}
	second, err := store.Update(context.Background(), addWorkspace(t, "two"))
	if err != nil {
		t.Fatalf("second Update error = %v, want writable store", err)
	}
	if second.WorkspaceCount() != 2 {
		t.Fatalf("second workspace count = %d, want 2", second.WorkspaceCount())
	}
}

func TestStoreCleansStaleTempsWithoutRepermissioningExistingConfig(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "swobu.yaml")
	if err := os.WriteFile(path, []byte("schema_version: 5\nworkspaces: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(dir, ".swobu.yaml.tmp-stale")
	_ = os.WriteFile(stale, []byte("partial"), 0o600)
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0644 {
		t.Fatalf("opening existing config rewrote its permissions: %04o", info.Mode().Perm())
	}
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale temp still exists: %v", err)
	}
	if _, err := store.Update(context.Background(), addWorkspace(t, "dev")); err != nil {
		t.Fatal(err)
	}
	// Open and Update succeed with an operator-selected 0644 starting file;
	// the atomic replacement retains its normal restrictive creation mode.
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestOpenAcceptsExistingDirectoryWithoutRepermissioning(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0750); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "swobu.yaml")
	if err := os.WriteFile(path, []byte("schema_version: 5\nworkspaces: {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	store, err := Open(path)
	if err != nil {
		t.Fatalf("trusted local directory was rejected: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if before.Mode().Perm() != after.Mode().Perm() {
		t.Fatal("Swobu rewrote existing directory permissions")
	}
}

func TestOpenDoesNotRepermissionExistingLockFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "swobu.yaml")
	if err := os.WriteFile(path, []byte("schema_version: 5\nworkspaces: {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".lock", nil, 0644); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path + ".lock")
	if err != nil {
		t.Fatal(err)
	}
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	after, err := os.Stat(path + ".lock")
	if err != nil {
		t.Fatal(err)
	}
	if before.Mode().Perm() != after.Mode().Perm() {
		t.Fatal("Swobu rewrote existing lock permissions")
	}
}
