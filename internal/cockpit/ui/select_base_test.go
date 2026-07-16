package ui

import (
	"testing"

	tui "github.com/grindlemire/go-tui"
)

func TestSelectBase_FocusLifecycle(t *testing.T) {
	base := NewSelectBase("test.1")
	base.Ref = nil
	app := &tui.App{}
	base.BindApp(app)

	if got := base.IsFocused(); got {
		t.Fatalf("IsFocused initially = %v, want false", got)
	}

	base.OnFocus(nil)
	if got := base.IsFocused(); !got {
		t.Fatalf("IsFocused after OnFocus = %v, want true", got)
	}

	base.OnBlur(nil)
	if got := base.IsFocused(); got {
		t.Fatalf("IsFocused after OnBlur = %v, want false", got)
	}
}

func TestSelectBase_ArrowMarker(t *testing.T) {
	base := NewSelectBase("test.2")
	base.Ref = nil

	base.OnFocus(nil)
	if got := base.Arrow(); got != SelectArrowFocused {
		t.Fatalf("Arrow when focused = %q, want %q", got, SelectArrowFocused)
	}

	base.OnBlur(nil)
	if got := base.Arrow(); got != SelectArrowBlurred {
		t.Fatalf("Arrow when blurred = %q, want %q", got, SelectArrowBlurred)
	}
}

func TestSelectBase_FocusedStateAccessor(t *testing.T) {
	base := NewSelectBase("test.3")
	if base.FocusedState() == nil {
		t.Fatal("FocusedState should expose the live state pointer")
	}

	base.OnFocus(nil)
	if got := base.FocusedState().Get(); !got {
		t.Fatalf("FocusedState after OnFocus = %v, want true", got)
	}

	base.OnBlur(nil)
	if got := base.FocusedState().Get(); got {
		t.Fatalf("FocusedState after OnBlur = %v, want false", got)
	}
}

func TestSelectBase_ID(t *testing.T) {
	base := NewSelectBase("section.slug")
	if got := base.ID; got != "section.slug" {
		t.Fatalf("ID = %q, want section.slug", got)
	}
}
