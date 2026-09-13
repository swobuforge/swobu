package ui

import (
	"strings"
	"testing"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/mountedrender"
	"github.com/swobuforge/swobu/internal/testkit/cockpittestkit"
)

func TestSelectNotEnteredHidesBodyAndEnteredShowsIt(t *testing.T) {
	s := NewSelect(SelectProps{
		ID:    "proto",
		Label: "protocol",
		Value: "",
		Body: func(backout func()) tui.Component {
			return NewSearchPicker("proto-picker", "protocol", []SearchOption{
				{ID: "responses", Label: "responses"},
			}, func(Selection) {}, backout)
		},
	})

	notEntered, err := mountedrender.String(s, 64, 12)
	if err != nil {
		t.Fatalf("not entered render: %v", err)
	}
	if !strings.Contains(notEntered, "protocol") || !strings.Contains(notEntered, "choose") {
		t.Fatalf("not entered row should show label + choose action:\n%s", notEntered)
	}
	if strings.Contains(notEntered, "responses") {
		t.Fatalf("not entered render must not include body options:\n%s", notEntered)
	}

	s.Enter()
	entered, err := mountedrender.String(s, 64, 12)
	if err != nil {
		t.Fatalf("entered render: %v", err)
	}
	if !strings.Contains(entered, "responses") {
		t.Fatalf("entered render should include body options:\n%s", entered)
	}

	s.Backout()
	again, err := mountedrender.String(s, 64, 12)
	if err != nil {
		t.Fatalf("backout render: %v", err)
	}
	if strings.Contains(again, "responses") {
		t.Fatalf("backout render must not include body options:\n%s", again)
	}
}

func TestSelectClosedActionActivatesWithoutEntering(t *testing.T) {
	activations := 0
	control := NewSelect(SelectProps{
		ID: "share", Label: "share", Value: "example.share.swobu.com", Action: "copy ↵",
		OnActivate: func() { activations++ },
	})
	harness, err := testkit.NewHarnessAt(control, 80, 6)
	if err != nil {
		t.Fatal(err)
	}
	defer harness.Close()
	harness.Open()
	harness.App().FocusNext()
	harness.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})

	if activations != 1 || control.IsEntered() {
		t.Fatalf("activations=%d entered=%v, want one activation and closed", activations, control.IsEntered())
	}
}
