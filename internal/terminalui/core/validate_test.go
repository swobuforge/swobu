package core

import (
	"strings"
	"testing"
)

func TestValidateCatchesDuplicateSiblingKeys(t *testing.T) {
	t.Parallel()

	root := Box[struct{}](
		Text[struct{}]("a").Key(K("dup")),
		Text[struct{}]("b").Key(K("dup")),
	)
	diags := Validate[struct{}](root)
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

	root := Box[struct{}](
		Box[struct{}](Text[struct{}]("a").Key(K("same"))),
		Box[struct{}](Text[struct{}]("b").Key(K("same"))),
	)
	diags := Validate[struct{}](root)
	if len(diags) != 0 {
		t.Fatalf("diagnostics = %#v, want none", diags)
	}
}

func TestValidateRejectsUnkeyedStatefulNode(t *testing.T) {
	t.Parallel()

	diags := Validate[struct{}](Text[struct{}]("draft").Stateful())
	if len(diags) != 1 {
		t.Fatalf("diagnostic count = %d, want 1", len(diags))
	}
	if !strings.Contains(diags[0].Message, "stateful node requires key") {
		t.Fatalf("message = %q, want stateful key requirement", diags[0].Message)
	}
}

func TestValidateRejectsInteractiveChildWithoutKeyInsideDynamicCollection(t *testing.T) {
	t.Parallel()

	root := Node[struct{}]{
		kind: KindList,
		children: []Node[struct{}]{
			Text[struct{}]("interactive child").Interaction(InteractionSpec[struct{}]{
				Focus:  FocusSpec{Mode: Focusable},
				Keymap: []KeyBindingSpec{{Pattern: KeyEnter(), Intent: IntentActivate}},
			}),
		},
	}
	diags := Validate[struct{}](root)
	if len(diags) != 1 {
		t.Fatalf("diagnostic count = %d, want 1", len(diags))
	}
	if !strings.Contains(diags[0].Message, "interactive child without key") {
		t.Fatalf("message = %q, want interactive key requirement", diags[0].Message)
	}
}

func TestValidatePermitsStaticUnkeyedChildrenInsideDynamicCollection(t *testing.T) {
	t.Parallel()

	root := Node[struct{}]{
		kind: KindList,
		children: []Node[struct{}]{
			Text[struct{}]("… 3 more"),
		},
	}
	diags := Validate[struct{}](root)
	if len(diags) != 0 {
		t.Fatalf("diagnostics = %#v, want none", diags)
	}
}

func TestValidatePermitsKeyedInteractiveChildrenInsideDynamicCollection(t *testing.T) {
	t.Parallel()

	root := Node[struct{}]{
		kind: KindList,
		children: []Node[struct{}]{
			Action[struct{}]("open", SignalEvent[struct{}]{Kind: "opened"}).Key(K("open")),
		},
	}
	diags := Validate[struct{}](root)
	if len(diags) != 0 {
		t.Fatalf("diagnostics = %#v, want none", diags)
	}
}

func TestValidateRejectsFocusableWithoutIntent(t *testing.T) {
	t.Parallel()

	root := Box[struct{}](Text[struct{}]("x").Key(K("bad-focus")).Interaction(InteractionSpec[struct{}]{Focus: FocusSpec{Mode: Focusable}}))

	diags := Validate[struct{}](root)
	if len(diags) != 1 {
		t.Fatalf("diagnostic count = %d, want 1", len(diags))
	}
	if diags[0].Severity != DiagnosticError {
		t.Fatalf("severity = %v, want DiagnosticError", diags[0].Severity)
	}
	if !strings.Contains(diags[0].Message, "focusable node without accepted intent") {
		t.Fatalf("message = %q, want focusable without intent", diags[0].Message)
	}
}

func TestValidateRejectsActionWithoutSignal(t *testing.T) {
	t.Parallel()

	root := Box[struct{}](Node[struct{}]{
		kind:    KindAction,
		content: ContentPayload{Text: "no-op"},
		interaction: InteractionSpec[struct{}]{
			Focus: FocusSpec{Mode: Focusable},
			Keymap: []KeyBindingSpec{
				{Pattern: KeyEnter(), Intent: IntentActivate},
			},
		},
	}.Key(K("no-signal")))

	diags := Validate[struct{}](root)
	if len(diags) != 1 {
		t.Fatalf("diagnostic count = %d, want 1", len(diags))
	}
	if diags[0].Severity != DiagnosticError {
		t.Fatalf("severity = %v, want DiagnosticError", diags[0].Severity)
	}
	if !strings.Contains(diags[0].Message, "action without emitted event") {
		t.Fatalf("message = %q, want action without signal", diags[0].Message)
	}
}

func TestValidateRejectsDuplicateFocusID(t *testing.T) {
	t.Parallel()

	root := Box[struct{}](
		Text[struct{}]("a").Interaction(InteractionSpec[struct{}]{
			Focus:  FocusSpec{Mode: Focusable, ID: FocusID("dup")},
			Keymap: []KeyBindingSpec{{Pattern: KeyEnter(), Intent: IntentActivate}},
		}),
		Text[struct{}]("b").Interaction(InteractionSpec[struct{}]{
			Focus:  FocusSpec{Mode: Focusable, ID: FocusID("dup")},
			Keymap: []KeyBindingSpec{{Pattern: KeyEnter(), Intent: IntentActivate}},
		}),
	)

	diags := Validate[struct{}](root)
	var focusIDDupDiags []Diagnostic
	for _, d := range diags {
		if strings.Contains(d.Message, `duplicate sibling focus ID "dup"`) {
			focusIDDupDiags = append(focusIDDupDiags, d)
		}
	}
	if len(focusIDDupDiags) != 1 {
		t.Fatalf("duplicate focus ID diagnostic count = %d, want 1; all diags = %#v", len(focusIDDupDiags), diags)
	}
	if focusIDDupDiags[0].Severity != DiagnosticError {
		t.Fatalf("severity = %v, want DiagnosticError", focusIDDupDiags[0].Severity)
	}
}

func TestValidateRejectsDuplicateFocusIDOnlyForFocusable(t *testing.T) {
	t.Parallel()

	root := Box[struct{}](
		Text[struct{}]("a").Interaction(InteractionSpec[struct{}]{
			Focus:  FocusSpec{Mode: Focusable, ID: FocusID("dup")},
			Keymap: []KeyBindingSpec{{Pattern: KeyEnter(), Intent: IntentActivate}},
		}),
		Text[struct{}]("b").Interaction(InteractionSpec[struct{}]{Focus: FocusSpec{Mode: FocusNone, ID: FocusID("dup")}}),
	)

	diags := Validate[struct{}](root)
	if len(diags) != 0 {
		t.Fatalf("diagnostics = %#v, want none", diags)
	}
}

func TestValidatePermitsValidAction(t *testing.T) {
	t.Parallel()

	root := Box[struct{}](Action[struct{}]("open", SignalEvent[struct{}]{Kind: "opened"}).Key(K("open")))
	diags := Validate[struct{}](root)
	if len(diags) != 0 {
		t.Fatalf("diagnostics = %#v, want none", diags)
	}
}

func TestValidateRejectsUnresolvedStyleToken(t *testing.T) {
	t.Parallel()

	root := Box[struct{}](Text[struct{}]("x").Key(K("bad-token")).Style(Style{Token: StyleToken("unknown.token")}))
	diags := Validate[struct{}](root)
	if len(diags) != 1 {
		t.Fatalf("diagnostic count = %d, want 1", len(diags))
	}
	if !strings.Contains(diags[0].Message, `unresolved style token "unknown.token"`) {
		t.Fatalf("message = %q, want unresolved style token", diags[0].Message)
	}
}

func TestValidateRejectsInvalidLayoutDimensions(t *testing.T) {
	t.Parallel()

	root := Box[struct{}](Text[struct{}]("x").Key(K("neg-dim")).Layout(Layout{
		Size: Size{Width: Fixed(-3), Height: Fit()},
	}))
	diags := Validate[struct{}](root)
	if len(diags) != 1 {
		t.Fatalf("diagnostic count = %d, want 1", len(diags))
	}
	if !strings.Contains(diags[0].Message, "invalid layout dimension") {
		t.Fatalf("message = %q, want invalid layout dimension", diags[0].Message)
	}
}

func TestValidateRejectsFocusableDisabledMismatch(t *testing.T) {
	t.Parallel()

	root := Box[struct{}](
		Text[struct{}]("x").
			Key(K("bad-focus")).
			Style(Style{State: StateDisabled}).
			Interaction(InteractionSpec[struct{}]{
				Focus:  FocusSpec{Mode: Focusable},
				Keymap: []KeyBindingSpec{{Pattern: KeyEnter(), Intent: IntentActivate}},
			}),
	)
	diags := Validate[struct{}](root)
	if len(diags) != 1 {
		t.Fatalf("diagnostic count = %d, want 1", len(diags))
	}
	if !strings.Contains(diags[0].Message, "focusable disabled mismatch") {
		t.Fatalf("message = %q, want focusable disabled mismatch", diags[0].Message)
	}
}

func TestValidateRejectsActionWithoutKey(t *testing.T) {
	t.Parallel()

	root := Box[struct{}](Action[struct{}]("open", SignalEvent[struct{}]{Kind: "opened"}))
	diags := Validate[struct{}](root)
	if len(diags) != 1 {
		t.Fatalf("diagnostic count = %d, want 1; diags=%#v", len(diags), diags)
	}
	if !strings.Contains(diags[0].Message, "action requires key") {
		t.Fatalf("message = %q, want action requires key", diags[0].Message)
	}
}

func TestValidateRejectsContractNameMissing(t *testing.T) {
	t.Parallel()

	root := Text[struct{}]("x").Contract(Contract[struct{}]{
		Signals: []SignalSpec[struct{}]{{Kind: "click"}},
	})
	diags := Validate[struct{}](root)
	wantMsg := "contract with fields set but no name"
	found := false
	for _, d := range diags {
		if strings.Contains(d.Message, wantMsg) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("diagnostics = %#v, want one with %q", diags, wantMsg)
	}
}

func TestValidateRejectsContractSignalCountMismatch(t *testing.T) {
	t.Parallel()

	root := Text[struct{}]("x").Contract(Contract[struct{}]{
		Name:    "Widget",
		Signals: []SignalSpec[struct{}]{{Kind: "a"}},
	}).Interaction(InteractionSpec[struct{}]{})
	diags := Validate[struct{}](root)
	wantMsg := "contract signals count 1 does not match interaction signals 0"
	found := false
	for _, d := range diags {
		if strings.Contains(d.Message, wantMsg) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("diagnostics = %#v, want one with %q", diags, wantMsg)
	}
}

func TestValidateRejectsContractFocusMismatch(t *testing.T) {
	t.Parallel()

	root := Text[struct{}]("x").Contract(Contract[struct{}]{
		Name:  "Widget",
		Focus: FocusPolicy{FocusableWhenEnabled: true},
	})
	diags := Validate[struct{}](root)
	wantMsg := "contract claims focusable but node is not focusable"
	found := false
	for _, d := range diags {
		if strings.Contains(d.Message, wantMsg) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("diagnostics = %#v, want one with %q", diags, wantMsg)
	}
}

func TestValidateRejectsContractDuplicateSignalKind(t *testing.T) {
	t.Parallel()

	root := Text[struct{}]("x").Contract(Contract[struct{}]{
		Name:    "Widget",
		Signals: []SignalSpec[struct{}]{{Kind: "dup"}, {Kind: "dup"}},
	}).Interaction(InteractionSpec[struct{}]{
		Signals: []SignalEvent[struct{}]{{Kind: "dup"}, {Kind: "dup"}},
	})
	diags := Validate[struct{}](root)
	wantMsg := `contract duplicate signal kind "dup"`
	found := false
	for _, d := range diags {
		if strings.Contains(d.Message, wantMsg) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("diagnostics = %#v, want one with %q", diags, wantMsg)
	}
}

func TestMinMaxRejectsDimFitMax(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("MinMax with DimFit max should panic")
		}
	}()
	_ = MinMax(5, Fit())
}
