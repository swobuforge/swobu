package core

import "testing"

func TestTextReturnsTextKindAndDefaults(t *testing.T) {
	t.Parallel()

	node := Text[struct{}]("hello")
	if got := node.Kind(); got != KindText {
		t.Fatalf("kind = %v, want KindText", got)
	}
	if got := node.ContentValue().Text; got != "hello" {
		t.Fatalf("content = %q, want hello", got)
	}
	if got := node.LayoutValue().Size.Width.Mode; got != DimFit {
		t.Fatalf("width mode = %v, want DimFit", got)
	}
	if got := node.LayoutValue().Size.Height.Mode; got != DimFit {
		t.Fatalf("height mode = %v, want DimFit", got)
	}
	if got := node.StyleValue().Token; got != TokenTextDefault {
		t.Fatalf("token = %q, want text.default", got)
	}
}

func TestBoxCopiesChildrenSlice(t *testing.T) {
	t.Parallel()

	children := []Node[struct{}]{Text[struct{}]("a"), Text[struct{}]("b")}
	box := Box[struct{}](children...)
	children[0] = Text[struct{}]("z")

	got := box.ChildrenValue()
	if len(got) != 2 {
		t.Fatalf("children len = %d, want 2", len(got))
	}
	if got[0].ContentValue().Text != "a" {
		t.Fatalf("first child = %q, want a", got[0].ContentValue().Text)
	}
	if got[1].ContentValue().Text != "b" {
		t.Fatalf("second child = %q, want b", got[1].ContentValue().Text)
	}

	got[0] = Text[struct{}]("changed")
	if box.ChildrenValue()[0].ContentValue().Text != "a" {
		t.Fatal("box children must be returned as a copy")
	}
}

func TestStackStoresVerticalFlow(t *testing.T) {
	t.Parallel()

	node := Stack[struct{}](AxisVertical, Text[struct{}]("a"))
	layout := node.LayoutValue()
	if got := layout.Flow.Mode; got != FlowStack {
		t.Fatalf("flow mode = %v, want FlowStack", got)
	}
	if got := layout.Flow.Axis; got != AxisVertical {
		t.Fatalf("axis = %v, want AxisVertical", got)
	}
}

func TestActionSeedsFocusableInteractionAndContract(t *testing.T) {
	t.Parallel()

	node := Action[struct{}]("open", SignalEvent[struct{}]{Kind: "opened"})
	if got := node.Kind(); got != KindAction {
		t.Fatalf("kind = %v, want KindAction", got)
	}
	if got := node.InteractionValue().Focus.Mode; got != Focusable {
		t.Fatalf("focus mode = %v, want Focusable", got)
	}
	if got := len(node.InteractionValue().Signals); got != 1 {
		t.Fatalf("signal count = %d, want 1", got)
	}
	if got := len(node.ContractValue().Signals); got != 1 {
		t.Fatalf("contract signals = %d, want 1", got)
	}
}

func TestScrollNodeConstruction(t *testing.T) {
	t.Parallel()

	child := Text[struct{}]("content")
	node := Scroll[struct{}](5, child)

	if got := node.Kind(); got != KindScroll {
		t.Fatalf("kind = %v, want KindScroll", got)
	}
	if got := node.ScrollOffsetValue(); got != 5 {
		t.Fatalf("scroll offset = %d, want 5", got)
	}
	children := node.ChildrenValue()
	if len(children) != 1 {
		t.Fatalf("children len = %d, want 1", len(children))
	}
	if got := children[0].ContentValue().Text; got != "content" {
		t.Fatalf("child content = %q, want content", got)
	}
	if got := node.LayoutValue().Size.Width.Mode; got != DimFill {
		t.Fatalf("width mode = %v, want DimFill", got)
	}
	if got := node.LayoutValue().Size.Height.Mode; got != DimFill {
		t.Fatalf("height mode = %v, want DimFill", got)
	}
}

func TestScrollOffsetModifier(t *testing.T) {
	t.Parallel()

	child := Text[struct{}]("a")
	node := Scroll[struct{}](0, child).ScrollOffset(10)

	if got := node.ScrollOffsetValue(); got != 10 {
		t.Fatalf("scroll offset = %d, want 10", got)
	}
}
