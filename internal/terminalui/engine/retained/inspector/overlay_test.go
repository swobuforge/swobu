package inspector

import (
	"os"
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/core"
)

func TestOverlay_WhenDisabled(t *testing.T) {
	os.Setenv("SWOBU_INSPECTOR", "")
	root := core.Text[struct{}]("hello")
	got := Overlay(ModeDiagnostics, root)
	if got.Kind() != core.KindText {
		t.Fatalf("overlay should return original tree when disabled")
	}
}

func TestOverlay_WhenEnabled(t *testing.T) {
	os.Setenv("SWOBU_INSPECTOR", "diagnostics")
	root := core.Text[struct{}]("hello")
	got := Overlay(ModeDiagnostics, root)
	if got.Kind() != core.KindLayer {
		t.Fatalf("overlay should return Layer when enabled, got %v", got.Kind())
	}
	children := got.ChildrenValue()
	if len(children) != 2 {
		t.Fatalf("layer children = %d, want 2", len(children))
	}
	if children[0].Kind() != core.KindText {
		t.Fatalf("first child should be original Text")
	}
	if children[1].Kind() != core.KindBox {
		t.Fatalf("second child should be inspector panel Box")
	}
}
