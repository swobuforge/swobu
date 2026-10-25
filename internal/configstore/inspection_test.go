package configstore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/swobuforge/swobu/internal/routing"
)

func TestInspectLockedPreservesDocumentAndSerializesPublication(t *testing.T) {
	store, path := openTestStore(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	inspecting := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- store.InspectLocked(context.Background(), func(config routing.Config) error {
			if config.WorkspaceCount() != 0 {
				return errors.New("unexpected inspection snapshot")
			}
			close(inspecting)
			<-release
			if config.WorkspaceCount() != 0 {
				return errors.New("inspection snapshot mutated")
			}
			return nil
		})
	}()
	<-inspecting
	// TryLock proves the publication lock is held without relying on timing or
	// interpreting a slow goroutine as evidence of mutual exclusion.
	if store.mu.TryLock() {
		store.mu.Unlock()
		close(release)
		<-done
		t.Fatal("inspection does not hold publication lock")
	}
	update := make(chan error, 1)
	edit := addWorkspace(t, "dev")
	go func() { _, err := store.Update(context.Background(), edit); update <- err }()
	after, err := os.ReadFile(path)
	if err != nil || string(before) != string(after) {
		t.Fatal("inspection changed document")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := <-update; err != nil {
		t.Fatal(err)
	}
	if store.Config().WorkspaceCount() != 1 {
		t.Fatal("waiting publication did not complete")
	}
}

func TestInspectLockedRejectsCanceledClosedAndFailedInspection(t *testing.T) {
	store, path := openTestStore(t)
	before, _ := os.ReadFile(path)
	failure := errors.New("dependent commit failed")
	if err := store.InspectLocked(context.Background(), func(routing.Config) error { return failure }); !errors.Is(err, failure) {
		t.Fatalf("inspection error: %v", err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("failed inspection rewrote routing")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	inspect := func(routing.Config) error { called = true; return nil }
	if err := store.InspectLocked(ctx, inspect); !errors.Is(err, context.Canceled) || called {
		t.Fatalf("canceled inspection: %v", err)
	}
	if err := store.InspectLocked(context.Background(), nil); err == nil {
		t.Fatal("accepted nil callback")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.InspectLocked(context.Background(), inspect); !errors.Is(err, ErrStoreClosed) || called {
		t.Fatalf("closed inspection: %v", err)
	}
}

func TestReferenceValidatorInstallsOnceAndGuardsEveryPublication(t *testing.T) {
	store, path := openTestStore(t)
	failure := errors.New("reference admission failed")
	if err := store.InstallReferenceValidator(func(routing.Config, *ReviewedConnection, func() error) error { return failure }); !errors.Is(err, failure) {
		t.Fatalf("current-state rejection: %v", err)
	}
	if err := store.InstallReferenceValidator(nil); err == nil {
		t.Fatal("accepted nil validator")
	}
	calls := 0
	if err := store.InstallReferenceValidator(func(config routing.Config, _ *ReviewedConnection, commit func() error) error {
		calls++
		if config.WorkspaceCount() != 0 {
			return failure
		}
		return commit()
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.InstallReferenceValidator(func(routing.Config, *ReviewedConnection, func() error) error { return nil }); err == nil {
		t.Fatal("replaced admission")
	}
	before, _ := os.ReadFile(path)
	if _, err := store.Update(context.Background(), addWorkspace(t, "dev")); !errors.Is(err, failure) {
		t.Fatalf("candidate rejection: %v", err)
	}
	after, _ := os.ReadFile(path)
	if calls != 2 || store.Config().WorkspaceCount() != 0 || string(before) != string(after) {
		t.Fatal("validator bypassed or failed admission published")
	}
}

// Reviewed mounts must fail closed when daemon assembly has not installed the
// Connection publication guard; neither disk nor retained retry state may move.
func TestReviewedMCPMountRequiresReferencePublisher(t *testing.T) {
	store, path := openTestStore(t)
	if _, err := store.Update(t.Context(), addWorkspace(t, "dev")); err != nil {
		t.Fatal(err)
	}
	workspace := store.Config().Workspaces()[0]
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.ApplyMCPMountChecked(t.Context(), workspace.Slug(), "docs", routing.MCPMount{ConnectionID: "cn_docs"}, fmt.Sprintf("%s-%d", workspace.ManagementIncarnation(), workspace.ManagementRevision()), "1", func(context.Context) error { return nil }, func(_ routing.Workspace, publish func() error) error { return publish() })
	if err == nil {
		t.Fatalf("unguarded publication: err=%v", err)
	}
	after, readErr := os.ReadFile(path)
	if readErr != nil || string(after) != string(before) || store.Config().Workspaces()[0].ManagementRevision() != workspace.ManagementRevision() {
		t.Fatal("unguarded mount changed routing or retry state")
	}
}
