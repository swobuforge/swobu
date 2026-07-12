package reconcile

import (
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/transcript"
)

func TestReconcile_Append_EmitsOnlyNewDurableLines(t *testing.T) {
	t.Parallel()

	prev := transcript.Group("root", transcript.DurableText("a"), transcript.DurableText("b"))
	next := transcript.Group("root", transcript.DurableText("a"), transcript.DurableText("b"), transcript.DurableText("c"))
	ops := Reconciler{}.Reconcile(prev, next, transcript.RenderModeAppend)
	if len(ops) != 1 || ops[0].Kind != RenderOpAppendDurableLine || ops[0].Text != "c" {
		t.Fatalf("unexpected ops: %#v", ops)
	}
}

func TestReconcile_Live_AppendsDurableAndUpdatesEphemeral(t *testing.T) {
	t.Parallel()

	prev := transcript.Group("root",
		transcript.DurableText("a"),
		transcript.EphemeralText("waiting"),
	)
	next := transcript.Group("root",
		transcript.DurableText("a"),
		transcript.DurableText("b"),
		transcript.EphemeralText("ready"),
	)
	ops := Reconciler{}.Reconcile(prev, next, transcript.RenderModeLive)
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

	prev := transcript.Group("root", transcript.DurableText("a"))
	next := transcript.Group("root", transcript.DurableText("a"), transcript.EphemeralText("status"))
	ops := Reconciler{}.Reconcile(prev, next, transcript.RenderModeFullscreen)
	if len(ops) != 1 || ops[0].Kind != RenderOpPaintFrame {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if len(ops[0].FrameLines) != 2 || ops[0].FrameLines[0] != "a" || ops[0].FrameLines[1] != "status" {
		t.Fatalf("unexpected frame lines: %#v", ops[0].FrameLines)
	}
}

func TestReconcileScene_Live_UsesProjectedSceneOnly(t *testing.T) {
	t.Parallel()

	prev := transcript.SceneSnapshot{Durable: []string{"a"}, Ephemeral: []string{"waiting"}}
	next := transcript.SceneSnapshot{Durable: []string{"a", "b"}, Ephemeral: []string{"ready"}}
	ops := Reconciler{}.ReconcileScene(prev, next, transcript.RenderModeLive)
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
