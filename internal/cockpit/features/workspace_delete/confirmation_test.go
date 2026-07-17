package workspace_delete

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
	"github.com/swobuforge/swobu/internal/cockpit/ui"
)

func TestConfirmation_EscapeUsesSharedBackScopeGrammar(t *testing.T) {
	src, err := os.ReadFile("confirmation.gsx")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.Contains(string(src), "tui.OnPreemptStop"+"(tui.KeyEscape") {
		t.Fatal("confirmation must express scope Escape through ui.BackScope, not bespoke preempt wiring")
	}
	if strings.Contains(string(src), "tui.OnFocused"+"(tui.KeyEscape") {
		t.Fatal("confirmation must not bind Escape as a shell-focused handler")
	}
	if !strings.Contains(string(src), "ui.BackScope(") {
		t.Fatal("confirmation must use the shared ui.BackScope grammar")
	}
}

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

func TestConfirmation_KeyMap_NilWhenClosed(t *testing.T) {
	confirmation := Confirmation(readmodel.WorkspaceReadModel{ID: "dev", Slug: "dev"}, nil, nil)
	if confirmation.KeyMap() != nil {
		t.Fatalf("confirmation.KeyMap should return nil when closed")
	}
	confirmation.Request("dev")
	if confirmation.KeyMap() == nil {
		t.Fatalf("confirmation.KeyMap should return Escape binding when open")
	}
}

func TestConfirmation_BackClosesOpenConfirmation(t *testing.T) {
	confirmation := Confirmation(readmodel.WorkspaceReadModel{ID: "dev", Slug: "dev"}, nil, nil)
	confirmation.Request("dev")

	if !confirmation.Back() {
		t.Fatal("Back should consume open confirmation")
	}
	if confirmation.IsOpen() {
		t.Fatal("confirmation should close")
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

	if got := ConfirmationRowComponent(confirmation).Phase(); got != ui.ConfirmFailed {
		t.Fatalf("phase = %v, want failed", got)
	}
	got := testkit.RenderMountedString(t, confirmation, 90, 5)
	assertRenderContains(t, got, "permission denied")
}

func TestConfirmation_RenderInlineStates(t *testing.T) {
	confirmation := Confirmation(
		readmodel.WorkspaceReadModel{ID: "dev", Slug: "dev"},
		func(context.Context, ports.DeleteWorkspaceRequest) error {
			return errors.New("permission denied")
		},
		nil,
	)
	got := testkit.RenderMountedString(t, confirmation, 90, 4)
	assertRenderContains(t, got, "delete", "workspace", "delete")

	confirmation.Request("dev")
	got = testkit.RenderMountedString(t, confirmation, 90, 4)
	assertRenderContains(t, got, "delete", "delete dev?", "confirm")

	confirmation.Confirm(context.Background())
	got = testkit.RenderMountedString(t, confirmation, 90, 5)
	assertRenderContains(t, got, "permission denied")
}

func TestConfirmation_FocusedRowShowsMarker(t *testing.T) {
	confirmation := Confirmation(readmodel.WorkspaceReadModel{ID: "dev", Slug: "dev"}, nil, nil)
	h, err := testkit.NewHarness(confirmation)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()

	h.Open()
	h.FocusNext()

	frame := h.Frame()
	testkit.AssertFocusedFrame(t, frame, "> delete")
	if !strings.Contains(frame, "delete ↵") {
		t.Fatalf("frame missing delete action label:\n%s", frame)
	}
}

func TestConfirmation_EscapeOnFocusedRow_SignalsBack(t *testing.T) {
	var cancelled bool
	confirmation := Confirmation(
		readmodel.WorkspaceReadModel{ID: "dev", Slug: "dev"},
		func(ctx context.Context, request ports.DeleteWorkspaceRequest) error {
			return nil
		},
		func(id readmodel.WorkspaceID) {},
	)
	confirmation.OnCancel = func(_ readmodel.WorkspaceID) { cancelled = true }

	// Arm via Request instead of Activate so the parent doesn't need wiring.
	confirmation.Request("dev")
	h, err := testkit.NewHarness(confirmation)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()
	h.FocusNext()
	if !confirmation.IsOpen() {
		t.Fatal("confirmation should be open before Escape")
	}

	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEscape})

	if confirmation.IsOpen() {
		t.Fatal("Escape should cancel confirmation")
	}
	if !cancelled {
		t.Fatal("OnCancel should have been called")
	}
}

func assertRenderContains(t *testing.T, got string, values ...string) {
	t.Helper()
	for _, value := range values {
		if !strings.Contains(got, value) {
			t.Fatalf("render should contain %q:\n%s", value, got)
		}
	}
}
