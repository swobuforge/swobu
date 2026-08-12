package configstore

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/swobuforge/swobu/internal/routing"
)

func openTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "swobu.yaml")
	if err := os.WriteFile(path, []byte("schema_version: 1\nworkspaces: {}\n"), 0o600); err != nil {
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
	if string(raw) != "schema_version: 1\nworkspaces: {}\n" {
		t.Fatalf("initial document = %q", raw)
	}
	if store.Config().WorkspaceCount() != 0 {
		t.Fatalf("workspace count = %d", store.Config().WorkspaceCount())
	}
	if _, err := OpenOrCreate(path); !errors.Is(err, ErrLocked) {
		t.Fatalf("second OpenOrCreate error = %v, want ErrLocked", err)
	}
	fileInfo, _ := os.Stat(path)
	dirInfo, _ := os.Stat(filepath.Dir(path))
	if fileInfo.Mode().Perm() != 0o600 || dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("modes file=%o dir=%o", fileInfo.Mode().Perm(), dirInfo.Mode().Perm())
	}
}

func storeTarget(t *testing.T, id string) routing.Target {
	t.Helper()
	targetID, _ := routing.ParseTargetID(id)
	model, _ := routing.ParseUpstreamModel("model-" + id)
	provider, _ := routing.ParseProvider("openai", func(raw string) bool { return raw == "openai" })
	connection, _ := routing.NewStandardConnection(provider, "", "env:OPENAI_API_KEY")
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
		id, _ := routing.ParseTargetID("dev")
		return config.UpdateTargetSettings(slug, route, id, routing.TargetSettings{
			Model: replacement.Model(), Protocol: replacement.Protocol(), Connection: replacement.Connection(),
		})
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
		t.Fatal("disk changed after failed UpdateTargetSettings persistence")
	}
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

func TestStoreUsesPrivatePermissionsAndCleansStaleTemps(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "swobu.yaml")
	if err := os.WriteFile(path, []byte("schema_version: 1\nworkspaces: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(dir, ".swobu.yaml.tmp-stale")
	_ = os.WriteFile(stale, []byte("partial"), 0o600)
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale temp still exists: %v", err)
	}
	if _, err := store.Update(context.Background(), addWorkspace(t, "dev")); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %o", info.Mode().Perm())
	}
	dirInfo, _ := os.Stat(dir)
	if dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode = %o", dirInfo.Mode().Perm())
	}
}

func TestOpenDoesNotMutateExistingUnsafeDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "swobu.yaml")
	if err := os.WriteFile(path, []byte("schema_version: 1\nworkspaces: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil {
		t.Fatal("Open unexpectedly accepted unsafe existing directory")
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o750 {
		t.Fatalf("directory mode mutated to %o", info.Mode().Perm())
	}
}

func TestOpenRepairsExistingLockFilePermissions(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "swobu.yaml")
	if err := os.WriteFile(path, []byte("schema_version: 1\nworkspaces: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".lock", nil, 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	info, err := os.Stat(path + ".lock")
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("lock mode = %o, want 600", info.Mode().Perm())
	}
}
