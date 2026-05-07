package views

import (
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/view"
)

func TestMessageBlock_ProjectsToPanelLines(t *testing.T) {
	t.Parallel()

	node := MessageBlock("Analysis", []string{
		"The alert is probably benign. The source IP belongs to a known cloud provider, and no reputation hits were found.",
		"",
		"Confidence: medium",
	}, 50)
	lines := view.DurableLines(node)
	if lines[0] != "╭─ Analysis ─────────────────────────────────────────╮" {
		t.Fatalf("top line mismatch: %q", lines[0])
	}
	if lines[len(lines)-1] != "╰────────────────────────────────────────────────────╯" {
		t.Fatalf("bottom line mismatch: %q", lines[len(lines)-1])
	}
}

func TestSplashBlock_ProjectsRows(t *testing.T) {
	t.Parallel()

	node := SplashBlock([]string{"a", "b"})
	lines := view.DurableLines(node)
	if len(lines) != 2 || lines[0] != "a" || lines[1] != "b" {
		t.Fatalf("unexpected lines=%#v", lines)
	}
}
