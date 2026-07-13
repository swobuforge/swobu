package core

import (
	"strings"
	"testing"
)

func TestValidateCatchesDuplicateSiblingKeys(t *testing.T) {
	t.Parallel()

	root := Box(
		Text("a").Key(K("dup")),
		Text("b").Key(K("dup")),
	)
	diags := Validate(root)
	if len(diags) != 1 {
		t.Fatalf("diagnostic count = %d, want 1", len(diags))
	}
	if diags[0].Severity != DiagnosticError {
		t.Fatalf("severity = %v, want DiagnosticError", diags[0].Severity)
	}
	if !strings.Contains(diags[0].Message, `duplicate sibling key "dup"`) {
		t.Fatalf("message = %q, want duplicate sibling key", diags[0].Message)
	}
}

func TestValidatePermitsSameKeyUnderDifferentParents(t *testing.T) {
	t.Parallel()

	root := Box(
		Box(Text("a").Key(K("same"))),
		Box(Text("b").Key(K("same"))),
	)
	diags := Validate(root)
	if len(diags) != 0 {
		t.Fatalf("diagnostics = %#v, want none", diags)
	}
}

func TestValidateRejectsUnkeyedStatefulNode(t *testing.T) {
	t.Parallel()

	diags := Validate(Text("draft").Stateful())
	if len(diags) != 1 {
		t.Fatalf("diagnostic count = %d, want 1", len(diags))
	}
	if !strings.Contains(diags[0].Message, "stateful node requires key") {
		t.Fatalf("message = %q, want stateful key requirement", diags[0].Message)
	}
}

func TestValidateRejectsInteractiveChildWithoutKeyInsideDynamicCollection(t *testing.T) {
	t.Parallel()

	root := Node{
		kind: KindList,
		children: []Node{
			Action("open", SignalEvent{Kind: "opened"}),
		},
	}
	diags := Validate(root)
	if len(diags) != 1 {
		t.Fatalf("diagnostic count = %d, want 1", len(diags))
	}
	if !strings.Contains(diags[0].Message, "interactive child without key") {
		t.Fatalf("message = %q, want interactive key requirement", diags[0].Message)
	}
}

func TestValidatePermitsStaticUnkeyedChildrenInsideDynamicCollection(t *testing.T) {
	t.Parallel()

	root := Node{
		kind: KindList,
		children: []Node{
			Text("… 3 more"),
		},
	}
	diags := Validate(root)
	if len(diags) != 0 {
		t.Fatalf("diagnostics = %#v, want none", diags)
	}
}

func TestValidatePermitsKeyedInteractiveChildrenInsideDynamicCollection(t *testing.T) {
	t.Parallel()

	root := Node{
		kind: KindList,
		children: []Node{
			Action("open", SignalEvent{Kind: "opened"}).Key(K("open")),
		},
	}
	diags := Validate(root)
	if len(diags) != 0 {
		t.Fatalf("diagnostics = %#v, want none", diags)
	}
}

func TestValidateRejectsFocusableWithoutIntent(t *testing.T) {
	t.Parallel()

	root := Box(Text("x").Key(K("bad-focus")).Interaction(InteractionSpec{Focus: FocusSpec{Mode: Focusable}}))

	diags := Validate(root)
	if len(diags) != 1 {
		t.Fatalf("diagnostic count = %d, want 1", len(diags))
	}
	if diags[0].Severity != DiagnosticWarning {
		t.Fatalf("severity = %v, want DiagnosticWarning", diags[0].Severity)
	}
	if !strings.Contains(diags[0].Message, "focusable node without accepted intent") {
		t.Fatalf("message = %q, want focusable without intent", diags[0].Message)
	}
}

func TestValidateRejectsActionWithoutSignal(t *testing.T) {
	t.Parallel()

	root := Box(Node{
		kind:    KindAction,
		content: ContentPayload{Text: "no-op"},
		interaction: InteractionSpec{
			Focus: FocusSpec{Mode: Focusable},
			Keymap: []KeyBindingSpec{
				{Pattern: KeyEnter(), Intent: IntentActivate},
			},
		},
	}.Key(K("no-signal")))

	diags := Validate(root)
	if len(diags) != 1 {
		t.Fatalf("diagnostic count = %d, want 1", len(diags))
	}
	if diags[0].Severity != DiagnosticWarning {
		t.Fatalf("severity = %v, want DiagnosticWarning", diags[0].Severity)
	}
	if !strings.Contains(diags[0].Message, "action without emitted event") {
		t.Fatalf("message = %q, want action without signal", diags[0].Message)
	}
}

func TestValidatePermitsValidAction(t *testing.T) {
	t.Parallel()

	root := Box(Action("open", SignalEvent{Kind: "opened"}).Key(K("open")))
	diags := Validate(root)
	if len(diags) != 0 {
		t.Fatalf("diagnostics = %#v, want none", diags)
	}
}
