package workspace_delete

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
)

func TestConfirmation_RequestBackAndCancel(t *testing.T) {
	confirmation := Confirmation(readmodel.WorkspaceReadModel{ID: "dev", Slug: "dev"}, nil, nil)

	confirmation.Request("dev")
	if !confirmation.IsOpen() {
		t.Fatal("confirmation should open")
	}
	if !confirmation.Back() {
		t.Fatal("Back should consume open confirmation")
	}
	if confirmation.IsOpen() {
		t.Fatal("confirmation should close")
	}

	confirmation.Request("dev")
	confirmation.Close()
	if confirmation.IsOpen() {
		t.Fatal("Close should cancel confirmation")
	}
}

func TestConfirmation_ActivateRequiresTwoEnters(t *testing.T) {
	var got ports.DeleteWorkspaceRequest
	var deleted readmodel.WorkspaceID
	confirmation := Confirmation(
		readmodel.WorkspaceReadModel{ID: "dev", Slug: "dev"},
		func(ctx context.Context, request ports.DeleteWorkspaceRequest) error {
			got = request
			return nil
		},
		func(id readmodel.WorkspaceID) {
			deleted = id
		},
	)

	confirmation.Activate()
	if !confirmation.IsOpen() {
		t.Fatal("first activation should arm confirmation")
	}
	if got.ID != "" {
		t.Fatalf("delete request after first activation = %+v, want empty", got)
	}

	confirmation.Activate()

	if got.ID != "dev" {
		t.Fatalf("delete request = %+v, want dev", got)
	}
	if deleted != "dev" {
		t.Fatalf("deleted callback = %q, want dev", deleted)
	}
	if confirmation.IsOpen() {
		t.Fatal("confirmation should close after success")
	}
}

func TestConfirmation_ConfirmFailureLeavesErrorVisible(t *testing.T) {
	confirmation := Confirmation(
		readmodel.WorkspaceReadModel{ID: "dev", Slug: "dev"},
		func(context.Context, ports.DeleteWorkspaceRequest) error {
			return errors.New("permission denied")
		},
		nil,
	)
	confirmation.Request("dev")

	confirmation.Confirm(context.Background())

	if confirmation.Phase.Get() != PhaseFailed {
		t.Fatalf("phase = %v, want failed", confirmation.Phase.Get())
	}
	if confirmation.Error.Get() != "permission denied" {
		t.Fatalf("error = %q, want permission denied", confirmation.Error.Get())
	}
}

func TestConfirmation_RenderInlineStates(t *testing.T) {
	confirmation := Confirmation(
		readmodel.WorkspaceReadModel{ID: "dev", Slug: "dev"},
		func(context.Context, ports.DeleteWorkspaceRequest) error {
			return errors.New("permission denied")
		},
		nil,
	)
	got := testkit.RenderString(confirmation.Render(nil), 90, 4)
	assertRenderContains(t, got, "delete", "workspace", "delete")

	confirmation.Request("dev")
	got = testkit.RenderString(confirmation.Render(nil), 90, 4)
	assertRenderContains(t, got, "delete", "delete dev?", "confirm")

	confirmation.Confirm(context.Background())
	got = testkit.RenderString(confirmation.Render(nil), 90, 5)
	assertRenderContains(t, got, "permission denied")
}

func assertRenderContains(t *testing.T, got string, values ...string) {
	t.Helper()
	for _, value := range values {
		if !strings.Contains(got, value) {
			t.Fatalf("render should contain %q:\n%s", value, got)
		}
	}
}
