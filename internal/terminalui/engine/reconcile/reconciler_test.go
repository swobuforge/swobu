package reconcile

import (
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/view"
)

func TestReconcile_Append_EmitsOnlyNewDurableLines(t *testing.T) {
	t.Parallel()

	prev := view.Group("root", view.DurableText("a"), view.DurableText("b"))
	next := view.Group("root", view.DurableText("a"), view.DurableText("b"), view.DurableText("c"))
	ops := Reconciler{}.Reconcile(prev, next, view.RenderModeAppend)
	if len(ops) != 1 || ops[0].Kind != RenderOpAppendDurableLine || ops[0].Text != "c" {
		t.Fatalf("unexpected ops: %#v", ops)
	}
}

func TestReconcile_Live_AppendsDurableAndUpdatesEphemeral(t *testing.T) {
	t.Parallel()

	prev := view.Group("root",
		view.DurableText("a"),
		view.EphemeralText("waiting"),
	)
	next := view.Group("root",
		view.DurableText("a"),
		view.DurableText("b"),
		view.EphemeralText("ready"),
	)
	ops := Reconciler{}.Reconcile(prev, next, view.RenderModeLive)
	if len(ops) != 2 {
		t.Fatalf("ops len=%d want 2 (%#v)", len(ops), ops)
	}
	if ops[0].Kind != RenderOpAppendDurableLine || ops[0].Text != "b" {
		t.Fatalf("unexpected append op: %#v", ops[0])
	}
	if ops[1].Kind != RenderOpUpdateEphemeralLine || ops[1].Text != "ready" {
		t.Fatalf("unexpected ephemeral op: %#v", ops[1])
	}
}

func TestReconcile_Fullscreen_EmitsFrameOnChange(t *testing.T) {
	t.Parallel()

	prev := view.Group("root", view.DurableText("a"))
	next := view.Group("root", view.DurableText("a"), view.EphemeralText("status"))
	ops := Reconciler{}.Reconcile(prev, next, view.RenderModeFullscreen)
	if len(ops) != 1 || ops[0].Kind != RenderOpPaintFrame {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if len(ops[0].FrameLines) != 2 || ops[0].FrameLines[0] != "a" || ops[0].FrameLines[1] != "status" {
		t.Fatalf("unexpected frame lines: %#v", ops[0].FrameLines)
	}
}

func TestReconcileScene_Live_UsesProjectedSceneOnly(t *testing.T) {
	t.Parallel()

	prev := view.SceneSnapshot{Durable: []string{"a"}, Ephemeral: []string{"waiting"}}
	next := view.SceneSnapshot{Durable: []string{"a", "b"}, Ephemeral: []string{"ready"}}
	ops := Reconciler{}.ReconcileScene(prev, next, view.RenderModeLive)
	if len(ops) != 2 {
		t.Fatalf("ops len=%d want 2 (%#v)", len(ops), ops)
	}
	if ops[0].Kind != RenderOpAppendDurableLine || ops[0].Text != "b" {
		t.Fatalf("unexpected append op: %#v", ops[0])
	}
	if ops[1].Kind != RenderOpUpdateEphemeralLine || ops[1].Text != "ready" {
		t.Fatalf("unexpected ephemeral op: %#v", ops[1])
	}
}
