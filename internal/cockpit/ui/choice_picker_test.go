package ui

import (
	"fmt"
	"strings"
	"testing"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
)

func TestChoicePickerRendersClosedSetWithoutSearch(t *testing.T) {
	picker := NewChoicePicker("routing", []ChoiceOption{
		{ID: "primary", Label: "primary"},
		{ID: "balance", Label: "balance with step 1"},
		{ID: "fallback", Label: "fallback 1"},
	}, "fallback", nil, nil)

	frame := testkit.RenderMountedTrimmed(t, picker, 80, 10)
	if strings.Contains(frame, "search") || strings.Contains(frame, "_") {
		t.Fatalf("closed choice picker rendered query grammar:\n%s", frame)
	}
	if !strings.Contains(frame, "> fallback 1") {
		t.Fatalf("selected value did not seed focus:\n%s", frame)
	}
}

func TestChoicePickerIgnoresTypingAndSelectsWithTraversal(t *testing.T) {
	selected := ""
	picker := NewChoicePicker("routing", []ChoiceOption{
		{ID: "primary", Label: "primary"},
		{ID: "balance", Label: "balance with step 1"},
		{ID: "fallback", Label: "fallback 1"},
	}, "fallback", func(value string) { selected = value }, nil)
	harness, err := testkit.NewHarnessAt(picker, 80, 10)
	if err != nil {
		t.Fatal(err)
	}
	defer harness.Close()
	harness.Open()

	harness.DispatchKey(tui.KeyEvent{Rune: 'p'})
	if frame := harness.FrameTrimmed(); strings.Contains(frame, "p_") || !strings.Contains(frame, "primary") {
		t.Fatalf("typing changed closed choices:\n%s", frame)
	}
	harness.DispatchKey(tui.KeyEvent{Key: tui.KeyUp})
	harness.DispatchKey(tui.KeyEvent{Key: tui.KeyUp})
	harness.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	if selected != "primary" {
		t.Fatalf("selected = %q, want primary", selected)
	}
}

func TestChoicePickerEscapeCancels(t *testing.T) {
	cancels := 0
	picker := NewChoicePicker("routing", []ChoiceOption{{ID: "primary", Label: "primary"}}, "primary", nil, func() { cancels++ })
	harness, err := testkit.NewHarnessAt(picker, 80, 8)
	if err != nil {
		t.Fatal(err)
	}
	defer harness.Close()
	harness.Open()

	harness.DispatchKey(tui.KeyEvent{Key: tui.KeyEscape})
	if cancels != 1 {
		t.Fatalf("cancel calls = %d, want 1", cancels)
	}
}

func TestChoicePickerPreservesTraversalAcrossSelectRerenders(t *testing.T) {
	selected := "fallback"
	selectControl := NewSelect(SelectProps{
		ID:     "routing-select",
		Label:  "routing",
		Action: "change ↵",
		Body: func(backout func()) tui.Component {
			return NewChoicePicker("routing-picker", []ChoiceOption{
				{ID: "primary", Label: "primary"},
				{ID: "balance", Label: "balance with step 1"},
				{ID: "fallback", Label: "fallback 1"},
			}, selected, func(value string) {
				selected = value
				backout()
			}, backout)
		},
	})
	harness, err := testkit.NewHarnessAt(selectControl, 80, 12)
	if err != nil {
		t.Fatal(err)
	}
	defer harness.Close()
	harness.Open()
	harness.App().FocusNext()
	harness.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	harness.DispatchKey(tui.KeyEvent{Key: tui.KeyUp})
	if frame := harness.FrameTrimmed(); !strings.Contains(frame, "> balance with step 1") {
		t.Fatalf("parent rerender reset closed-choice traversal:\n%s", frame)
	}
	harness.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	if selected != "balance" || selectControl.IsEntered() {
		t.Fatalf("selected=%q entered=%v, want balance and closed", selected, selectControl.IsEntered())
	}
}

func TestChoicePickerRevealsSelectedValueBeyondInitialWindow(t *testing.T) {
	options := make([]ChoiceOption, 10)
	for i := range options {
		options[i] = ChoiceOption{ID: fmt.Sprintf("choice-%d", i), Label: fmt.Sprintf("choice %d", i)}
	}
	picker := NewChoicePicker("long", options, "choice-8", nil, nil)
	frame := testkit.RenderMountedTrimmed(t, picker, 80, 10)
	if !strings.Contains(frame, "> choice 8") || strings.Contains(frame, "> choice 0") {
		t.Fatalf("off-window selection not revealed:\n%s", frame)
	}
}
