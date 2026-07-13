package views

import (
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/app/operator/clientprofile"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/core"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/corelower"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/paint"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
)

func TestBuildClientPickerOptionNode_ReturnsCoreNode(t *testing.T) {
	t.Parallel()

	profiles := clientprofile.Catalog()
	choice := selectedClientProfile(profiles, "claude")
	if choice == nil {
		t.Fatal("expected client profile choice")
	}

	node := buildClientPickerOptionNode(choice)
	if diags := core.Validate(node); len(diags) != 0 {
		t.Fatalf("validation failed: %#v", diags)
	}
	if got, want := node.KeyValue().String(), clientPickerFocusKey(choice); got != want {
		t.Fatalf("key = %q, want %q", got, want)
	}

	interactionValue := node.InteractionValue()
	if got := len(interactionValue.Keymap); got != 1 {
		t.Fatalf("keymap count = %d, want 1", got)
	}
	if got, want := interactionValue.Keymap[0].Pattern.Name, "noop"; got != want {
		t.Fatalf("keymap[0] pattern = %q, want %q", got, want)
	}
	if got, want := interactionValue.Keymap[0].Intent, core.IntentActivate; got != want {
		t.Fatalf("keymap[0] intent = %q, want %q", got, want)
	}

	if got := len(interactionValue.Signals); got != 1 {
		t.Fatalf("signal count = %d, want 1", got)
	}
	activationSignal := interactionValue.Signals[0]
	if got, want := activationSignal.Kind, cockpitActionSignalKind; got != want {
		t.Fatalf("activation signal kind = %q, want %q", got, want)
	}
	activationEvent, ok := activationSignal.Event.(state.SetSelectedClientID)
	if !ok {
		t.Fatalf("activation signal event = %T, want state.SetSelectedClientID", activationSignal.Event)
	}
	if got, want := activationEvent.ID, choice.Identity().ID; got != want {
		t.Fatalf("activation id = %q, want %q", got, want)
	}

	if got := len(interactionValue.FocusSignals); got != 1 {
		t.Fatalf("focus signal count = %d, want 1", got)
	}
	focusSignal := interactionValue.FocusSignals[0]
	if got, want := focusSignal.Kind, cockpitRowFocusSignalKind; got != want {
		t.Fatalf("focus signal kind = %q, want %q", got, want)
	}
	focusEvent, ok := focusSignal.Event.(state.SetFocusedRowAffordance)
	if !ok {
		t.Fatalf("focus signal event = %T, want state.SetFocusedRowAffordance", focusSignal.Event)
	}
	if focusEvent.Verb != "select" {
		t.Fatalf("focus signal verb = %q, want select", focusEvent.Verb)
	}
	if focusEvent.AllowSpace {
		t.Fatal("focus signal should not allow space")
	}

	renderNode, err := corelower.Lower(node, corelower.EnvConfig{}, func(a state.Action) update.Action {
		return a
	})
	if err != nil {
		t.Fatalf("lower: %v", err)
	}
	if renderNode == nil {
		t.Fatal("expected render node")
	}

	tree := (&layout.TreeBuilder{}).Build(renderNode, geom.Rect{W: 80, H: 2})
	buf := paint.NewBuffer(geom.Rect{W: 80, H: 2})
	paintLayoutTree(tree, buf, &layout.PaintContext{}, geom.Point{})
	out := strings.TrimSpace(buf.String())

	for _, want := range []string{choice.Identity().Label} {
		if !strings.Contains(out, want) {
			t.Fatalf("render = %q, want %q", out, want)
		}
	}
}
