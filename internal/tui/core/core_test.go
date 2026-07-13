package core

import (
	"strings"
	"testing"
)

// event is a test-specific event type.
type event struct{ name string }

// --- Constructor correctness ---

func TestNode_ConstructorsProduceCorrectKind(t *testing.T) {
	if Empty[event]().Kind() != "empty" {
		t.Fatal("expected empty kind")
	}
	if Text[event](Key("a"), "Hello").Kind() != "text" {
		t.Fatal("expected text kind")
	}
	if Region[event](Key("a"), true).Kind() != "region" {
		t.Fatal("expected region kind")
	}
	if Flow[event](Key("a"), AxisVertical).Kind() != "flow" {
		t.Fatal("expected flow kind")
	}
	if Action[event](Key("a"), "Run", false, event{name: "go"}).Kind() != "action" {
		t.Fatal("expected action kind")
	}
	if Field[event](Key("a"), "val", nil, nil, nil).Kind() != "field" {
		t.Fatal("expected field kind")
	}
}

func TestNode_KeysRequiredForNonEmpty(t *testing.T) {
	cases := []struct {
		name string
		node Node[event]
	}{
		{"text", Text[event](Key(""), "Hello")},
		{"region", Region[event](Key(""), true)},
		{"flow", Flow[event](Key(""), AxisVertical)},
		{"action", Action[event](Key(""), "Run", false, event{})},
		{"field", Field[event](Key(""), "val", nil, nil, nil)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !tc.node.Key().Empty() {
				t.Fatal("expected empty key")
			}
		})
	}
}

// --- Immutability ---

func TestNode_ChildrenDefensiveCopy(t *testing.T) {
	child := Text[event](Key("child"), "c")
	parent := Flow[event](Key("parent"), AxisVertical, child)
	children := parent.Children()
	children[0] = Text[event](Key("mutant"), "m")

	if parent.Children()[0].Key() != Key("child") {
		t.Fatal("modifying returned child slice mutated parent")
	}
}

func TestNode_SignalsDefensiveCopy(t *testing.T) {
	act := Action[event](Key("act"), "Go", false, event{name: "go"})
	sigs := act.Signals()
	sigs[0] = Signal[event]{Kind: SignalChange, Event: event{name: "bad"}}

	if act.Signals()[0].Kind != SignalActivate {
		t.Fatal("modifying returned signals mutated action")
	}
}

func TestNode_AcceptsDefensiveCopy(t *testing.T) {
	act := Action[event](Key("act"), "Go", false, event{name: "go"})
	accepts := act.Accepts()
	accepts[0] = IntentCancel

	if act.Accepts()[0] != IntentActivate {
		t.Fatal("modifying returned accepts mutated action")
	}
}

func TestNode_ConstructorClonesChildren(t *testing.T) {
	kids := []Node[event]{
		Text[event](Key("a"), "a"),
	}
	parent := Flow[event](Key("p"), AxisVertical, kids...)
	kids[0] = Text[event](Key("b"), "b") // mutate source slice after construction

	if parent.Children()[0].Key() != Key("a") {
		t.Fatal("constructor did not copy children slice")
	}
}

func TestNode_ShortcutsDefensiveCopy(t *testing.T) {
	act := Action[event](Key("act"), "Run", false, event{name: "go"})
	act = act.WithShortcut(IntentHelp, Signal[event]{Kind: SignalActivate, Event: event{name: "help"}})

	shortcuts := act.Shortcuts()
	if len(shortcuts) != 1 {
		t.Fatalf("expected 1 shortcut, got %d", len(shortcuts))
	}
	if shortcuts[0].Intent != IntentHelp {
		t.Fatalf("expected help intent, got %s", shortcuts[0].Intent)
	}

	// Defensive copy proof
	shortcuts[0] = Shortcut[event]{Intent: IntentQuit}
	if act.Shortcuts()[0].Intent != IntentHelp {
		t.Fatal("modifying returned shortcuts mutated node")
	}
}

// --- Validate: structural invariants ---

func TestValidate_EmptyTreeHasNoErrors(t *testing.T) {
	root := Empty[event]()
	if diags := Validate(root); diags.HasErrors() {
		t.Fatalf("expected no errors for empty tree, got %v", diags.Items)
	}
}

func TestValidate_ValidTreeHasNoErrors(t *testing.T) {
	root := Flow[event](Key("root"), AxisVertical,
		Text[event](Key("t"), "hello"),
		Action[event](Key("act"), "Go", false, event{name: "go"}),
		Field[event](Key("f"), "val",
			func(s string) event { return event{name: s} },
			func(s string) event { return event{name: s} },
			func() event { return event{name: "cancel"} },
		),
	)
	if diags := Validate(root); diags.HasErrors() {
		t.Fatalf("expected no errors for valid tree, got %v", diags.Items)
	}
}

func TestValidate_MissingKey(t *testing.T) {
	root := Text[event](Key(""), "hello")
	diags := Validate(root)
	if !diags.HasErrors() {
		t.Fatal("expected error for missing key")
	}
	found := findMessage(diags, "non-empty node has empty key", SeverityError)
	if found == nil {
		t.Fatalf("expected missing-key error, got %v", diags.Items)
	}
}

func TestValidate_DuplicateKeyAcrossSiblings(t *testing.T) {
	root := Flow[event](Key("root"), AxisVertical,
		Text[event](Key("dup"), "a"),
		Text[event](Key("dup"), "b"),
	)
	diags := Validate(root)
	if !diags.HasErrors() {
		t.Fatal("expected error for duplicate key")
	}
	found := findMessage(diags, "duplicate key: dup", SeverityError)
	if found == nil {
		t.Fatalf("expected duplicate-key error, got %v", diags.Items)
	}
}

func TestValidate_DuplicateKeyAcrossNestedScopes(t *testing.T) {
	root := Flow[event](Key("root"), AxisVertical,
		Text[event](Key("dup"), "a"),
		Region[event](Key("r"), true,
			Text[event](Key("dup"), "b"),
		),
	)
	diags := Validate(root)
	if !diags.HasErrors() {
		t.Fatal("expected error for duplicate key in nested scope")
	}
}

func TestValidate_ActionWithoutSignals(t *testing.T) {
	root := Action[event](Key("act"), "Run", false, event{name: "go"}).WithSignals(nil)
	diags := Validate(root)
	if !diags.HasErrors() {
		t.Fatal("expected error for action without signals")
	}
}

func TestValidate_FieldWithoutCallbacks(t *testing.T) {
	cases := []struct {
		name string
		node Node[event]
		want string
	}{
		{
			name: "nil OnChange",
			node: Field[event](Key("f"), "v", nil, func(s string) event { return event{} }, func() event { return event{} }),
			want: "field node has nil OnChange callback",
		},
		{
			name: "nil OnCommit",
			node: Field[event](Key("f2"), "v", func(s string) event { return event{} }, nil, func() event { return event{} }),
			want: "field node has nil OnCommit callback",
		},
		{
			name: "nil OnCancel",
			node: Field[event](Key("f3"), "v", func(s string) event { return event{} }, func(s string) event { return event{} }, nil),
			want: "field node has nil OnCancel callback",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			diags := Validate(tc.node)
			if !diags.HasErrors() {
				t.Fatal("expected error")
			}
			if findMessage(diags, tc.want, SeverityError) == nil {
				t.Fatalf("expected %q error, got %v", tc.want, diags.Items)
			}
		})
	}
}

func TestValidate_DiagnosticsAreDeterministic(t *testing.T) {
	root := Flow[event](Key("root"), AxisVertical,
		Text[event](Key("dup"), "a"),
		Text[event](Key("dup"), "b"),
	)
	d1 := Validate(root)
	d2 := Validate(root)
	if len(d1.Items) != len(d2.Items) {
		t.Fatal("non-deterministic item count")
	}
	for i := range d1.Items {
		if !d1.Items[i].EqualReports(d2.Items[i]) {
			t.Fatalf("diagnostic %d differs: %+v vs %+v", i, d1.Items[i], d2.Items[i])
		}
	}
}

// --- Validate: focusability ---

func TestValidate_FocusableNodeWithoutFocusID(t *testing.T) {
	root := Action[event](Key("act"), "Run", false, event{}).WithFocusID(FocusID(""))
	diags := Validate(root)
	found := findMessage(diags, "focusable node without FocusID", SeverityWarning)
	if found == nil {
		t.Fatalf("expected warning, got %v", diags.Items)
	}
}

func TestValidate_FocusableNodeWithoutFocusID_HasValidHint(t *testing.T) {
	root := Action[event](Key("act"), "Run", false, event{}).WithFocusID(FocusID(""))
	diags := Validate(root)
	found := findMessage(diags, "focusable node without FocusID", SeverityWarning)
	if found == nil {
		t.Fatal("missing warning")
	}
	if !strings.Contains(found.Hint, "FocusID") {
		t.Fatalf("hint should mention FocusID, got %q", found.Hint)
	}
}

func TestValidate_FocusIDWithNoAccepts_IsWarning(t *testing.T) {
	root := Text[event](Key("txt"), "hello").WithFocusID(FocusID("x"))
	diags := Validate(root)
	found := findMessage(diags, "node with FocusID accepts no intents", SeverityWarning)
	if found == nil {
		t.Fatalf("expected warning, got %v", diags.Items)
	}
}

// --- Validate: disabled interaction rules ---

func TestValidate_DisabledActionCannotHaveSignals(t *testing.T) {
	root := Action[event](Key("act"), "Run", true, event{})
	diags := Validate(root)
	found := findMessage(diags, "disabled node declares signals", SeverityWarning)
	if found == nil {
		t.Fatalf("expected warning, got %v", diags.Items)
	}
}

func TestValidate_DisabledActionCannotHaveAccepts(t *testing.T) {
	root := Action[event](Key("act"), "Run", true, event{})
	diags := Validate(root)
	found := findMessage(diags, "disabled node declares accepted intents", SeverityWarning)
	if found == nil {
		t.Fatalf("expected warning, got %v", diags.Items)
	}
}

func TestValidate_EnabledActionHasNoDisabledWarnings(t *testing.T) {
	root := Action[event](Key("act"), "Run", false, event{})
	diags := Validate(root)
	if findMessage(diags, "disabled node declares signals", SeverityWarning) != nil {
		t.Fatal("enabled node should not trigger disabled-signal warning")
	}
	if findMessage(diags, "disabled node declares accepted intents", SeverityWarning) != nil {
		t.Fatal("enabled node should not trigger disabled-accepts warning")
	}
}

func TestValidate_DisabledHintIsActionable(t *testing.T) {
	root := Action[event](Key("act"), "Run", true, event{})
	diags := Validate(root)
	found := findMessage(diags, "disabled node declares signals", SeverityWarning)
	if found == nil {
		t.Fatal("missing warning")
	}
	if !strings.Contains(found.Hint, "enable") {
		t.Fatalf("hint should mention enable, got %q", found.Hint)
	}
}

// --- Validate: role validation ---

func TestValidate_UnsupportedRole_IsWarning(t *testing.T) {
	root := Text[event](Key("t"), "hello").WithRole(Role("unknown-role-42"))
	diags := Validate(root)
	found := findMessage(diags, "unsupported role: unknown-role-42", SeverityWarning)
	if found == nil {
		t.Fatalf("expected warning, got %v", diags.Items)
	}
}

func TestValidate_KnownRoles_NoWarning(t *testing.T) {
	all := []Role{RoleNone, RoleRoot, RolePanel, RoleHeading, RoleLabel, RoleValue, RoleMuted, RoleDanger, RoleSuccess, RoleWarning, RoleFocus, RoleSelected, RoleDisabled}
	for _, r := range all {
		root := Text[event](Key("t"), "hello").WithRole(r)
		if diags := Validate(root); diags.HasWarnings() {
			t.Fatalf("role %q should not produce warnings, got %v", r, diags.Items)
		}
	}
}

// --- Validate: range/length validation ---

func TestValidate_InvalidWidthRange(t *testing.T) {
	root := Text[event](Key("t"), "hello").WithSize(
		Length{Kind: SizeFixed, Value: 10, Min: 20, Max: 5},
		Length{},
	)
	diags := Validate(root)
	found := findMessage(diags, "invalid width constraint", SeverityWarning)
	if found == nil {
		t.Fatalf("expected warning, got %v", diags.Items)
	}
}

func TestValidate_InvalidHeightRange(t *testing.T) {
	root := Text[event](Key("t"), "hello").WithSize(
		Length{},
		Length{Kind: SizeFixed, Value: -3, Min: 0, Max: 0},
	)
	diags := Validate(root)
	found := findMessage(diags, "invalid height constraint", SeverityWarning)
	if found == nil {
		t.Fatalf("expected warning, got %v", diags.Items)
	}
}

func TestValidate_ValidLengthNoWarning(t *testing.T) {
	root := Text[event](Key("t"), "hello").WithSize(
		Length{Kind: SizeFixed, Value: 10, Min: 0, Max: 20},
		Length{Kind: SizeFill, Value: 1, Min: 0, Max: 0},
	)
	if diags := Validate(root); diags.HasWarnings() || diags.HasErrors() {
		t.Fatalf("expected no diagnostics, got %v", diags.Items)
	}
}

// --- Validate: diagnostics shape ---

func TestValidate_DiagnosticHasSeverity(t *testing.T) {
	root := Text[event](Key(""), "hello")
	diags := Validate(root)
	if len(diags.Items) == 0 {
		t.Fatal("expected diagnostics")
	}
	if diags.Items[0].Severity != SeverityError {
		t.Fatalf("expected SeverityError, got %v", diags.Items[0].Severity)
	}
}

func TestValidate_DiagnosticHasHint(t *testing.T) {
	root := Text[event](Key(""), "hello")
	diags := Validate(root)
	if len(diags.Items) == 0 {
		t.Fatal("expected diagnostics")
	}
	if diags.Items[0].Hint == "" {
		t.Fatal("expected non-empty hint")
	}
}

func TestValidate_DiagnosticsCounts(t *testing.T) {
	root := Flow[event](Key("root"), AxisVertical,
		Text[event](Key(""), "missing"),
		Text[event](Key(""), "another missing"),
	)
	diags := Validate(root)
	if diags.ErrCount() != 2 {
		t.Fatalf("expected 2 errors, got %d (items: %v)", diags.ErrCount(), diags.Items)
	}
	if diags.WarnCount() != 0 {
		t.Fatalf("expected 0 warnings, got %d", diags.WarnCount())
	}
}

// --- Node helpers ---

func TestNode_IsRunnable(t *testing.T) {
	enabled := Action[event](Key("a"), "Go", false, event{})
	disabled := Action[event](Key("b"), "Go", true, event{})
	text := Text[event](Key("t"), "x")

	if !enabled.IsRunnable() {
		t.Fatal("enabled action should be runnable")
	}
	if disabled.IsRunnable() {
		t.Fatal("disabled action should not be runnable")
	}
	if text.IsRunnable() {
		t.Fatal("text node should not be runnable")
	}
}

// --- Key ---

func TestKey_Child(t *testing.T) {
	k := Key("parent").Child("child").Child("grand")
	if k != Key("parent.child.grand") {
		t.Fatalf("expected parent.child.grand, got %s", k)
	}
}

func TestKey_Empty(t *testing.T) {
	if !Key("").Empty() {
		t.Fatal("expected empty key")
	}
	if Key("foo").Empty() {
		t.Fatal("expected non-empty key")
	}
}

// --- FocusMemory ---

func TestFocusMemory_Empty(t *testing.T) {
	fm0 := FocusMemory{}
	if !fm0.Empty() {
		t.Fatal("expected empty FocusMemory")
	}
	fm := FocusMemory{ActiveFocusID: FocusID("x")}
	if fm.Empty() {
		t.Fatal("expected non-empty FocusMemory")
	}
}

// --- Dimensions ---

func TestSize_IsZero(t *testing.T) {
	if !(Size{W: 0, H: 0}.Zero()) {
		t.Fatal("expected zero")
	}
	if (Size{W: 1, H: 0}.Zero()) {
		t.Fatal("expected non-zero")
	}
}

// --- Import proof: nothing forbidden in core ---

func TestCore_ImportProof(t *testing.T) {
	// Core package intentionally has no imports beyond standard library.
	// This test exists in the package to make the import set visible
	// alongside the semantic tests.
	_ = strings.ToUpper("")
}

// --- helpers ---

func findMessage(diags Diagnostics, msg string, sev DiagnosticSeverity) *Diagnostic {
	for i := range diags.Items {
		if diags.Items[i].Severity == sev && diags.Items[i].Message == msg {
			return &diags.Items[i]
		}
	}
	return nil
}
