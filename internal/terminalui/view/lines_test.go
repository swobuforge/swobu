package view

import "testing"

func TestDurableLines_RendersPanelNode(t *testing.T) {
	t.Parallel()

	root := Group("root", DurablePanel(PanelSpec{
		Title:       "Note",
		Rows:        []string{"hello world"},
		TargetWidth: 24,
		MinWidth:    20,
		PadLeft:     1,
		PadRight:    1,
	}))
	lines := DurableLines(root)
	if len(lines) < 3 {
		t.Fatalf("panel lines=%d want >=3", len(lines))
	}
	if lines[0] != "╭─ Note ───────────────╮" {
		t.Fatalf("top=%q", lines[0])
	}
	if lines[len(lines)-1] != "╰──────────────────────╯" {
		t.Fatalf("bottom=%q", lines[len(lines)-1])
	}
}

func TestDurableLines_RendersFlowColumnGap(t *testing.T) {
	t.Parallel()

	root := FlowColumn("root", 1, DurableText("a"), DurableText("b"))
	lines := DurableLines(root)
	if len(lines) != 3 {
		t.Fatalf("lines=%d want 3", len(lines))
	}
	if lines[0] != "a" || lines[1] != "" || lines[2] != "b" {
		t.Fatalf("unexpected lines=%#v", lines)
	}
}

func TestDurableLines_ShowWhenFalseHidesChildren(t *testing.T) {
	t.Parallel()

	root := FlowColumn("root", 0,
		DurableText("visible"),
		ShowWhen("hidden", false, DurableText("secret")),
	)
	lines := DurableLines(root)
	if len(lines) != 1 || lines[0] != "visible" {
		t.Fatalf("lines=%#v", lines)
	}
}

func TestDurableLines_RendersGridRows(t *testing.T) {
	t.Parallel()

	root := GridLayout("grid", 2, 1,
		DurableText("a"),
		DurableText("b"),
		DurableText("c"),
	)
	lines := DurableLines(root)
	if len(lines) != 2 {
		t.Fatalf("lines=%d want 2", len(lines))
	}
	if lines[0] != "a b" || lines[1] != "c" {
		t.Fatalf("lines=%#v", lines)
	}
}

func TestDurableLines_ScrollYAppliesOffset(t *testing.T) {
	t.Parallel()

	root := ScrollY("scroll", 1, FlowColumn("content", 0, DurableText("l1"), DurableText("l2"), DurableText("l3")))
	lines := DurableLines(root)
	if len(lines) != 2 || lines[0] != "l2" || lines[1] != "l3" {
		t.Fatalf("lines=%#v", lines)
	}
}
