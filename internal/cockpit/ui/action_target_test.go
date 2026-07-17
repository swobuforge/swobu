package ui

import (
	"testing"

	tui "github.com/grindlemire/go-tui"
)

func TestActionTarget_KeyMapBindsLocalEscape(t *testing.T) {
	escapes := 0
	target := NewActionTarget("action.test", nil)

	escape := bindingForKey(t, target.KeyMap(nil, func() { escapes++ }), tui.KeyEscape)
	escape.Handler(tui.KeyEvent{Key: tui.KeyEscape})

	if escapes != 1 {
		t.Fatalf("escapes = %d, want 1", escapes)
	}
}

func TestActionTarget_ShellOptionsDoesNotOverwriteKeyMapEscape(t *testing.T) {
	escapes := 0
	target := NewActionTarget("action.test", nil)

	keymap := target.KeyMap(nil, func() { escapes++ })
	_ = target.ShellOptions()
	escape := bindingForKey(t, keymap, tui.KeyEscape)
	escape.Handler(tui.KeyEvent{Key: tui.KeyEscape})

	if escapes != 1 {
		t.Fatalf("escapes = %d, want 1", escapes)
	}
}
