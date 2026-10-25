package authplane

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	workspaceapi "github.com/swobuforge/swobu/internal/app/operator/workspaces"
	"github.com/swobuforge/swobu/internal/configstore"
)

// Authentication establishes a credential result, not consent to change a
// persisted Route. Synthetic opaque refs exercise no real credential material.
func TestSuccessfulAuthPollingCannotChangeRouting(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "swobu.yaml")
	if err := os.WriteFile(path, []byte("schema_version: 5\nworkspaces: {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := configstore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service, err := workspaceapi.NewService(store, workspaceapi.Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	target := workspaceapi.TargetDraft{ID: "primary", Model: "fixture", Protocol: "responses", ConnectionID: "cn_original"}
	_, _, err = service.CreateWorkspaceChecked(t.Context(), workspaceapi.CreateWorkspace{Slug: "research", InitialRoute: "chat", Target: target}, "fixture-operator", "create-fixture", func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewAuthSessionManager(driverStub{
		startFn: func(context.Context, StartInput) (DriverStartResult, error) {
			return DriverStartResult{SessionID: "fixture-session"}, nil
		},
		pollFn: func(context.Context, string) (DriverPollResult, error) {
			return DriverPollResult{State: SessionStateSucceeded, CredentialRef: "env:AUTH_RESULT_FIXTURE"}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = manager.Start(t.Context(), StartInput{ProviderSpec: "openai", Workspace: "research", Route: "chat", TargetID: "primary", AuthMode: "browser"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := manager.Poll(t.Context(), "fixture-session")
	if err != nil {
		t.Fatal(err)
	}
	if result.CredentialRef != "env:AUTH_RESULT_FIXTURE" {
		t.Fatal("authentication result lost")
	}
	if _, err := manager.Poll(t.Context(), "fixture-session"); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("successful poll changed persisted Routing without retained revision/consent")
	}
}
