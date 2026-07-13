package views

import (
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/core"
)

func TestNewAnchoredDisclosureNode_ReturnsStack(t *testing.T) {
	t.Parallel()

	parent := core.Text[any]("parent")
	d1 := core.Text[any]("detail-1")
	d2 := core.Text[any]("detail-2")

	node := NewAnchoredDisclosureNode(parent, d1, d2)
	if node.Kind() != core.KindStack {
		t.Fatalf("Kind = %v, want KindStack", node.Kind())
	}
	children := node.ChildrenValue()
	if len(children) != 3 {
		t.Fatalf("children = %d, want 3", len(children))
	}
	if children[0].ContentValue().Text != "parent" {
		t.Fatalf("child[0] = %q, want parent", children[0].ContentValue().Text)
	}
	if children[1].ContentValue().Text != "detail-1" {
		t.Fatalf("child[1] = %q, want detail-1", children[1].ContentValue().Text)
	}
}

func TestNewAnchoredDisclosureNode_OmitsEmptyDetails(t *testing.T) {
	t.Parallel()

	parent := core.Text[any]("parent")
	empty := core.Box[any]()

	node := NewAnchoredDisclosureNode(parent, empty)
	children := node.ChildrenValue()
	if len(children) != 1 {
		t.Fatalf("children = %d, want 1 (empty detail omitted)", len(children))
	}
}

func TestNewAnchoredDisclosureNode_DetailInset(t *testing.T) {
	t.Parallel()

	node := NewAnchoredDisclosureNode(core.Text[any]("p"), core.Text[any]("d"))
	children := node.ChildrenValue()
	if children[1].LayoutValue().Inset.Left != 2 {
		t.Fatalf("detail inset = %d, want 2", children[1].LayoutValue().Inset.Left)
	}
}
