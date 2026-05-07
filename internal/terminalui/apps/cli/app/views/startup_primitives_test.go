package views

import (
	"strings"
	"testing"
)

func TestRenderTextPanelLines_DefaultStyle(t *testing.T) {
	t.Parallel()

	spec := defaultMessagePanelSpec("Analysis", []string{
		"The alert is probably benign. The source IP belongs to a known cloud provider, and no reputation hits were found.",
		"",
		"Confidence: medium",
	}, 50)
	lines := RenderTextPanel(spec, WrappedTextRowsView(spec.Body))

	if lines[0] != "╭─ Analysis ─────────────────────────────────────────╮" {
		t.Fatalf("top line mismatch: %q", lines[0])
	}
	if lines[len(lines)-1] != "╰────────────────────────────────────────────────────╯" {
		t.Fatalf("bottom line mismatch: %q", lines[len(lines)-1])
	}
}

func TestRenderTextPanelLines_WidthConstraintsAndPadding(t *testing.T) {
	t.Parallel()

	spec := TextPanelSpec{
		Title:         "X",
		Body:          []string{"a b c d e f g"},
		TargetWidth:   200,
		MinWidth:      20,
		MaxWidth:      30,
		HorizontalPad: 2,
		Border:        defaultMessagePanelBorderStyle(),
	}
	lines := RenderTextPanel(spec, WrappedTextRowsView(spec.Body))
	for i, line := range lines {
		if got := len([]rune(line)); got != 30 {
			t.Fatalf("line[%d] width=%d want=30 line=%q", i, got, line)
		}
	}
	if lines[1] != "│  a b c d e f g             │" {
		t.Fatalf("body line with padding mismatch: %q", lines[1])
	}
}

func TestRenderTextPanelLines_CustomBorderStyle(t *testing.T) {
	t.Parallel()

	spec := TextPanelSpec{
		Title:         "Flow summary",
		Body:          []string{"This flow is ready."},
		TargetWidth:   32,
		MinWidth:      20,
		HorizontalPad: 1,
		Border: TextPanelBorderStyle{
			TopLeft:      "+",
			TopRight:     "+",
			BottomLeft:   "+",
			BottomRight:  "+",
			Horizontal:   "-",
			Vertical:     "|",
			TitlePrefix:  "[ ",
			TitleSuffix:  " ]",
			FallbackName: "Panel",
		},
	}
	lines := RenderTextPanel(spec, WrappedTextRowsView(spec.Body))
	if lines[0] != "+[ Flow summary ]--------------+" {
		t.Fatalf("custom top line mismatch: %q", lines[0])
	}
	if lines[len(lines)-1] != "+------------------------------+" {
		t.Fatalf("custom bottom line mismatch: %q", lines[len(lines)-1])
	}
}

func TestRenderTextPanel_ComposesStackedContentViews(t *testing.T) {
	t.Parallel()

	spec := defaultMessagePanelSpec("Flow", nil, 40)
	content := StackPanelContentViews(
		WrappedTextRowsView([]string{"First block line"}),
		WrappedTextRowsView([]string{""}),
		WrappedTextRowsView([]string{"Second block line"}),
	)
	lines := RenderTextPanel(spec, content)
	if len(lines) < 5 {
		t.Fatalf("unexpected panel output: %#v", lines)
	}
	if !strings.Contains(lines[1], "First block line") {
		t.Fatalf("first composed block missing: %#v", lines)
	}
	if !strings.Contains(lines[3], "Second block line") {
		t.Fatalf("second composed block missing: %#v", lines)
	}
	if lines[2] == lines[1] || lines[2] == lines[3] {
		t.Fatalf("expected blank spacer row between composed blocks; lines=%#v", lines)
	}
}
