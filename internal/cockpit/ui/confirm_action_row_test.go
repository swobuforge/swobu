package ui

import (
	"errors"
	"os"
	"strings"
	"testing"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
)

func TestConfirmActionRow_DoesNotOwnRawFocusedEscapeBinding(t *testing.T) {
	src, err := os.ReadFile("confirm_action_row.go")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.Contains(string(src), "tui.OnFocused"+"(tui.KeyEscape") {
		t.Fatal("ConfirmActionRow must express local Escape through ui.ActionTarget, not raw go-tui dispatch")
	}
}

func TestConfirmActionRow_EscapeClosesConfirmWithoutActing(t *testing.T) {
	acts := 0
	row := NewConfirmActionRow("delete.route", ConfirmActionCopy{
		Label:           "delete",
		IdleValue:       "model route",
		IdleAction:      "delete ↵",
		ConfirmValue:    "delete gpt?",
		ConfirmAction:   "confirm ↵",
		SubmittingValue: "deleting gpt…",
		SubmittingHint:  "wait",
		FailedValue:     "delete failed",
		FailedAction:    "retry ↵",
	}, func() error {
		acts++
		return nil
	})

	h, err := testkit.NewHarness(row)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()
	h.App().FocusNext()
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	frame := h.Frame()
	if !strings.Contains(frame, "delete gpt?") || !strings.Contains(frame, "confirm ↵") {
		t.Fatalf("expected armed confirm state:\n%s", frame)
	}
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEscape})
	if acts != 0 {
		t.Fatalf("acts after Escape = %d, want 0", acts)
	}
	if row.Phase() != ConfirmIdle {
		t.Fatalf("phase after Escape = %v, want idle", row.Phase())
	}
}

func TestConfirmActionRow_FailedConfirmShowsRetryAndError(t *testing.T) {
	row := NewConfirmActionRow("delete.route", ConfirmActionCopy{
		Label:           "delete",
		IdleValue:       "model route",
		IdleAction:      "delete ↵",
		ConfirmValue:    "delete gpt?",
		ConfirmAction:   "confirm ↵",
		SubmittingValue: "deleting gpt…",
		SubmittingHint:  "wait",
		FailedValue:     "delete failed",
		FailedAction:    "retry ↵",
	}, func() error {
		return errors.New("boom")
	})

	h, err := testkit.NewHarness(row)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()
	h.App().FocusNext()
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	frame := h.Frame()
	if !strings.Contains(frame, "delete failed") || !strings.Contains(frame, "retry ↵") || !strings.Contains(frame, "boom") {
		t.Fatalf("expected local failure state:\n%s", frame)
	}
}
