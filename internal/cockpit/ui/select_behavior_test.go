package ui

import (
	"testing"

	tui "github.com/grindlemire/go-tui"
)

// TestSelect_EnterActivatesBody verifies that Enter activates the select body.
func TestSelect_EnterActivatesBody(t *testing.T) {
	onEnterCalled := false
	s := NewSelect(SelectProps{
		ID:    "test",
		Label: "test",
		Value: "",
		OnEnter: func() {
			onEnterCalled = true
		},
		Body: func(backout func()) tui.Component {
			return NewSelectableRow("body", "body content", "", "", nil)
		},
	})

	s.Enter()
	if !onEnterCalled {
		t.Fatal("Enter should call OnEnter")
	}
	if !s.IsEntered() {
		t.Fatal("Enter should set entered state to true")
	}
}

// TestSelect_SecondActivationBacksOut verifies that activating while entered backs out.
func TestSelect_SecondActivationBacksOut(t *testing.T) {
	s := NewSelect(SelectProps{
		ID:    "test",
		Label: "test",
		Value: "",
		Body: func(backout func()) tui.Component {
			return NewSelectableRow("body", "body content", "", "", nil)
		},
	})

	// First activation enters
	s.activate()
	if !s.IsEntered() {
		t.Fatal("First activation should enter")
	}

	// Second activation backs out
	s.activate()
	if s.IsEntered() {
		t.Fatal("Second activation should back out")
	}
}

// TestSelect_EscapeOnHeaderBacksOut verifies Escape on header backs out.
func TestSelect_EscapeOnHeaderBacksOut(t *testing.T) {
	s := NewSelect(SelectProps{
		ID:    "test",
		Label: "test",
		Value: "",
		Body: func(backout func()) tui.Component {
			return NewSelectableRow("body", "body content", "", "", nil)
		},
	})

	// Enter the select
	s.Enter()
	if !s.IsEntered() {
		t.Fatal("Enter should set entered state")
	}

	// Get the header row and call its OnEscape
	header := s.headerRow()
	if header.OnEscape == nil {
		t.Fatal("Header OnEscape should be set while entered")
	}

	header.OnEscape()
	if s.IsEntered() {
		t.Fatal("Escape should back out from entered state")
	}
}

// TestSelect_CanEnterFalseRefusesEntry verifies that CanEnter=false refuses entry.
func TestSelect_CanEnterFalseRefusesEntry(t *testing.T) {
	s := NewSelect(SelectProps{
		ID:    "test",
		Label: "test",
		Value: "",
		CanEnter: func() bool {
			return false
		},
		Body: func(backout func()) tui.Component {
			return NewSelectableRow("body", "body content", "", "", nil)
		},
	})

	s.Enter()
	if s.IsEntered() {
		t.Fatal("CanEnter=false should refuse entry")
	}

	// Also verify activate refuses entry
	s.activate()
	if s.IsEntered() {
		t.Fatal("activate() should respect CanEnter=false")
	}
}

// TestSelect_OnEnterFiresOncePerEnter verifies OnEnter fires once per enter.
func TestSelect_OnEnterFiresOncePerEnter(t *testing.T) {
	onEnterCount := 0
	s := NewSelect(SelectProps{
		ID:    "test",
		Label: "test",
		Value: "",
		OnEnter: func() {
			onEnterCount++
		},
		Body: func(backout func()) tui.Component {
			return NewSelectableRow("body", "body content", "", "", nil)
		},
	})

	// First enter
	s.Enter()
	if onEnterCount != 1 {
		t.Fatalf("OnEnter should fire once on first enter, got %d", onEnterCount)
	}

	// Second enter (no-op since already entered)
	s.Enter()
	if onEnterCount != 1 {
		t.Fatalf("OnEnter should not fire again, got %d", onEnterCount)
	}

	// Backout and re-enter
	s.Backout()
	s.Enter()
	if onEnterCount != 2 {
		t.Fatalf("OnEnter should fire once on re-enter, got %d", onEnterCount)
	}
}

// TestSelect_OnBackoutFiresOncePerBackout verifies OnBackout fires once per backout.
func TestSelect_OnBackoutFiresOncePerBackout(t *testing.T) {
	onBackoutCount := 0
	s := NewSelect(SelectProps{
		ID:    "test",
		Label: "test",
		Value: "",
		OnBackout: func() {
			onBackoutCount++
		},
		Body: func(backout func()) tui.Component {
			return NewSelectableRow("body", "body content", "", "", nil)
		},
	})

	// Backout without entering (no-op)
	s.Backout()
	if onBackoutCount != 0 {
		t.Fatalf("OnBackout should not fire when not entered, got %d", onBackoutCount)
	}

	// Enter and backout
	s.Enter()
	s.Backout()
	if onBackoutCount != 1 {
		t.Fatalf("OnBackout should fire once on backout, got %d", onBackoutCount)
	}

	// Second backout (no-op)
	s.Backout()
	if onBackoutCount != 1 {
		t.Fatalf("OnBackout should not fire again, got %d", onBackoutCount)
	}
}

// TestSelect_HeaderOnlyHandlesEscapeWhileEntered verifies header only handles Escape while entered.
func TestSelect_HeaderOnlyHandlesEscapeWhileEntered(t *testing.T) {
	s := NewSelect(SelectProps{
		ID:    "test",
		Label: "test",
		Value: "",
		Body: func(backout func()) tui.Component {
			return NewSelectableRow("body", "body content", "", "", nil)
		},
	})

	// When not entered, header should not handle Escape
	header := s.headerRow()
	if header.OnEscape != nil {
		t.Fatal("Header OnEscape should be nil when not entered")
	}

	// When entered, header should handle Escape
	s.Enter()
	header = s.headerRow()
	if header.OnEscape == nil {
		t.Fatal("Header OnEscape should be set when entered")
	}

	// After backout, header should not handle Escape
	s.Backout()
	header = s.headerRow()
	if header.OnEscape != nil {
		t.Fatal("Header OnEscape should be nil after backout")
	}
}

// TestSelect_BodyReceivesBackoutCallback verifies Body receives backout callback.
func TestSelect_BodyReceivesBackoutCallback(t *testing.T) {
	var receivedBackout func()
	s := NewSelect(SelectProps{
		ID:    "test",
		Label: "test",
		Value: "",
		Body: func(backout func()) tui.Component {
			receivedBackout = backout
			return NewSelectableRow("body", "body content", "", "", nil)
		},
	})

	// Enter the select
	s.Enter()
	if !s.IsEntered() {
		t.Fatal("Should be entered")
	}

	// Access Body directly to verify it receives backout callback
	if s.props.Body == nil {
		t.Fatal("Body should not be nil")
	}

	// Call Body to get the callback
	bodyComponent := s.props.Body(s.Backout)
	if bodyComponent == nil {
		t.Fatal("Body component should not be nil")
	}

	if receivedBackout == nil {
		t.Fatal("Body should receive backout callback")
	}

	// Verify the backout callback works
	if s.IsEntered() {
		receivedBackout()
		if s.IsEntered() {
			t.Fatal("Received backout callback should exit entered state")
		}
	}
}

// TestSelect_IsEnteredReportsState verifies IsEntered reports correct state.
func TestSelect_IsEnteredReportsState(t *testing.T) {
	s := NewSelect(SelectProps{
		ID:    "test",
		Label: "test",
		Value: "",
		Body: func(backout func()) tui.Component {
			return NewSelectableRow("body", "body content", "", "", nil)
		},
	})

	// Initially not entered
	if s.IsEntered() {
		t.Fatal("Should not be entered initially")
	}

	// After Enter, should be entered
	s.Enter()
	if !s.IsEntered() {
		t.Fatal("Should be entered after Enter()")
	}

	// After backout, should not be entered
	s.Backout()
	if s.IsEntered() {
		t.Fatal("Should not be entered after backout()")
	}
}

// TestSelect_BackoutIdempotent verifies backout is idempotent when not entered.
func TestSelect_BackoutIdempotent(t *testing.T) {
	onBackoutCount := 0
	s := NewSelect(SelectProps{
		ID:    "test",
		Label: "test",
		Value: "",
		OnBackout: func() {
			onBackoutCount++
		},
		Body: func(backout func()) tui.Component {
			return NewSelectableRow("body", "body content", "", "", nil)
		},
	})

	// Multiple backouts when not entered should be safe
	s.Backout()
	s.Backout()
	s.Backout()

	if onBackoutCount != 0 {
		t.Fatal("OnBackout should not fire when not entered")
	}
	if s.IsEntered() {
		t.Fatal("Should not be entered after backouts")
	}
}

// TestSelect_EnterIdempotent verifies Enter is mostly idempotent.
func TestSelect_EnterIdempotent(t *testing.T) {
	onEnterCount := 0
	s := NewSelect(SelectProps{
		ID:    "test",
		Label: "test",
		Value: "",
		OnEnter: func() {
			onEnterCount++
		},
		Body: func(backout func()) tui.Component {
			return NewSelectableRow("body", "body content", "", "", nil)
		},
	})

	// First Enter
	s.Enter()
	if onEnterCount != 1 {
		t.Fatal("OnEnter should fire once")
	}

	// Second Enter (no-op)
	s.Enter()
	if onEnterCount != 1 {
		t.Fatal("OnEnter should not fire again when already entered")
	}

	if !s.IsEntered() {
		t.Fatal("Should still be entered")
	}
}
