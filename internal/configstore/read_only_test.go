package configstore

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/swobuforge/swobu/internal/routing"
)

func TestExternallyManagedConfigOpenNeverWritesOrLocks(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "config-map")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "swobu.yaml")
	initial := []byte("schema_version: 5\nworkspaces: {}\n")
	if err := os.WriteFile(path, initial, 0644); err != nil {
		t.Fatal(err)
	}

	store, err := OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	if !store.ReadOnly() || store.Config().WorkspaceCount() != 0 {
		t.Fatal("read-only snapshot not opened")
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// No writes are allowed even for a valid semantic edit.
	_, err = store.Update(context.Background(), func(routing.Config) (routing.Config, error) {
		t.Fatal("read-only mutation callback must not execute")
		return routing.Config{}, nil
	})
	if !errors.Is(err, ErrReadOnly) {
		t.Fatalf("mutation: %v", err)
	}
	_, err = store.UpdatePrepared(context.Background(), func(routing.Config) (routing.Config, error) {
		t.Fatal("read-only prepared mutation must not execute")
		return routing.Config{}, nil
	}, func(routing.Config, routing.Config) error {
		t.Fatal("pre-commit effects must not execute")
		return nil
	})
	if !errors.Is(err, ErrReadOnly) {
		t.Fatalf("prepared mutation: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(initial, got) || before.ModTime() != after.ModTime() || before.Mode() != after.Mode() {
		t.Fatal("read-only open/close/mutation attempt changed file")
	}
	if _, err := os.Stat(path + ".lock"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read-only open created lock: %v", err)
	}
}

func TestExternallyManagedConfigRejectsInvalidFileWithoutEffects(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "external.yaml")
	for _, contents := range []string{
		"schema_version: 999\nworkspaces: {}\n",
		"schema_version: 5\nworkspaces: {}\nunrecognized: true\n",
		"schema_version: 5\nworkspaces: []\n",
	} {
		if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := OpenReadOnly(path); err == nil {
			t.Fatalf("accepted invalid document %q", contents)
		}
		if _, err := os.Stat(path + ".lock"); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("invalid open created lock")
		}
	}
}

func TestExternallyManagedConfigCanBeReadWhileManagedWriterIsLocked(t *testing.T) {
	writer, path := openTestStore(t)
	reader, err := OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if _, err := writer.Update(context.Background(), addWorkspace(t, "dev")); err != nil {
		t.Fatal(err)
	}
	// External mode is a restart/snapshot boundary, not a watcher.
	if reader.Config().WorkspaceCount() != 0 {
		t.Fatal("read-only snapshot unexpectedly auto-reloaded")
	}
}

func TestExternallyManagedConfigCheckedMutationsDoNotAdmitEffects(t *testing.T) {
	path := filepath.Join(t.TempDir(), "desired.yaml")
	if err := os.WriteFile(path, []byte("schema_version: 5\nworkspaces: {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	store, err := OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	slug, err := routing.ParseWorkspaceSlug("new")
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.SetWorkspaceDisplayNameChecked(context.Background(), slug, "New", "stale", func(context.Context) error {
		t.Fatal("read-only admission callback executed")
		return nil
	})
	if !errors.Is(err, ErrReadOnly) {
		t.Fatalf("checked read-only mutation: %v", err)
	}
	if _, err := os.Stat(path + ".lock"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("checked operation created lock")
	}
}
