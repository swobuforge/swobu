package ui

import (
	"errors"
	"strings"
	"testing"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
)

type flowTextFixture struct {
	text string
}

func (f flowTextFixture) Render(*tui.App) *tui.Element {
	col := tui.New(
		tui.WithDisplay(tui.DisplayFlex),
		tui.WithDirection(tui.Column),
		tui.WithWidthPercent(100),
	)
	col.AddChild(FlowText(f.text).Root)
	col.AddChild(tui.New(tui.WithText("SENTINEL")))
	return col
}

type indentedFlowTextFixture struct {
	paddingLeft int
	text        string
}

func (f indentedFlowTextFixture) Render(*tui.App) *tui.Element {
	col := tui.New(
		tui.WithDisplay(tui.DisplayFlex),
		tui.WithDirection(tui.Column),
		tui.WithWidthPercent(100),
	)
	block := tui.New(
		tui.WithWidthPercent(100),
		tui.WithPaddingTRBL(0, 0, 0, f.paddingLeft),
	)
	block.AddChild(FlowText(f.text).Root)
	col.AddChild(block)
	col.AddChild(tui.New(tui.WithText("SENTINEL")))
	return col
}

func TestFlowTextGeometryContract(t *testing.T) {
	t.Parallel()

	longSentence := "The quick brown fox jumps over the lazy dog and discovers that text flows losslessly across the viewport."

	t.Run("height reflows inversely with width", func(t *testing.T) {
		t.Parallel()

		rendered30 := testkit.RenderMountedTrimmed(t, flowTextFixture{text: longSentence}, 30, 20)
		rendered60 := testkit.RenderMountedTrimmed(t, flowTextFixture{text: longSentence}, 60, 20)
		rendered100 := testkit.RenderMountedTrimmed(t, flowTextFixture{text: longSentence}, 100, 20)

		lines30 := countNonEmptyLines(rendered30)
		lines60 := countNonEmptyLines(rendered60)
		lines100 := countNonEmptyLines(rendered100)

		if lines30 <= lines60 || lines60 < lines100 {
			t.Fatalf("expected lines to reflow with width: lines30=%d, lines60=%d, lines100=%d\n30:\n%s\n60:\n%s\n100:\n%s",
				lines30, lines60, lines100, rendered30, rendered60, rendered100)
		}

		// Ensure all words are present (no truncation).
		words := strings.Fields(longSentence)
		for _, w := range words {
			if !strings.Contains(rendered30, w) {
				t.Fatalf("rendered30 missing word %q:\n%s", w, rendered30)
			}
			if !strings.Contains(rendered60, w) {
				t.Fatalf("rendered60 missing word %q:\n%s", w, rendered60)
			}
			if !strings.Contains(rendered100, w) {
				t.Fatalf("rendered100 missing word %q:\n%s", w, rendered100)
			}
		}
	})

	t.Run("sentinel is positioned strictly after the text block", func(t *testing.T) {
		t.Parallel()

		for _, width := range []int{30, 60, 100} {
			rendered := testkit.RenderMountedTrimmed(t, flowTextFixture{text: longSentence}, width, 20)
			lines := strings.Split(strings.TrimRight(rendered, "\n"), "\n")
			if len(lines) < 2 {
				t.Fatalf("expected at least 2 lines at width %d, got:\n%s", width, rendered)
			}
			lastLine := lines[len(lines)-1]
			if !strings.Contains(lastLine, "SENTINEL") {
				t.Fatalf("SENTINEL is not on the last line at width %d:\n%s", width, rendered)
			}
			// Sentinel should not appear in any preceding line.
			for i := 0; i < len(lines)-1; i++ {
				if strings.Contains(lines[i], "SENTINEL") {
					t.Fatalf("SENTINEL leaked into line %d at width %d:\n%s", i, width, rendered)
				}
			}
		}
	})

	t.Run("preserves explicit newlines", func(t *testing.T) {
		t.Parallel()

		multiline := "line one\nline two\nline three"
		rendered := testkit.RenderMountedTrimmed(t, flowTextFixture{text: multiline}, 80, 10)

		if !strings.Contains(rendered, "line one") ||
			!strings.Contains(rendered, "line two") ||
			!strings.Contains(rendered, "line three") {
			t.Fatalf("missing explicit newline content:\n%s", rendered)
		}
		lines := strings.Split(strings.TrimRight(rendered, "\n"), "\n")
		// Line 1: line one, Line 2: line two, Line 3: line three, Line 4: SENTINEL
		if len(lines) != 4 {
			t.Fatalf("expected 4 lines for 3 multiline entries + sentinel, got %d:\n%s", len(lines), rendered)
		}
	})

	t.Run("unbroken long URLs wrap losslessly without losing characters", func(t *testing.T) {
		t.Parallel()

		longURL := "https://bedrock-mantle.eu-west-2.api.aws/openai/v1/deployments/anthropic.claude-3-5-sonnet-20241022-v2:0/chat/completions"
		rendered := testkit.RenderMountedTrimmed(t, flowTextFixture{text: longURL}, 40, 15)

		// Stripping whitespace and newlines must reconstruct the exact unbroken URL.
		reconstructed := strings.ReplaceAll(strings.ReplaceAll(rendered, "\n", ""), " ", "")
		if !strings.Contains(reconstructed, longURL) {
			t.Fatalf("unbroken URL was truncated or corrupted:\nOriginal: %s\nReconstructed: %s\nRendered:\n%s",
				longURL, reconstructed, rendered)
		}
	})

	t.Run("unicode and wide characters do not corrupt layout", func(t *testing.T) {
		t.Parallel()

		unicodeText := "🔑 AWS Credential: arn:aws:iam::123456789012:role/SwobuExecutionRole ⛉ [verified]"
		rendered := testkit.RenderMountedTrimmed(t, flowTextFixture{text: unicodeText}, 50, 10)

		if !strings.Contains(rendered, "🔑") || !strings.Contains(rendered, "⛉") || !strings.Contains(rendered, "SwobuExecutionRole") {
			t.Fatalf("unicode text corrupted or missing:\n%s", rendered)
		}
		if !strings.Contains(rendered, "SENTINEL") {
			t.Fatalf("SENTINEL missing in unicode render:\n%s", rendered)
		}
	})

	t.Run("indented flow text inside block with padding", func(t *testing.T) {
		t.Parallel()

		errText := "validation failed: invalid endpoint scheme 'ftp://', expected http or https"
		rendered := testkit.RenderMountedTrimmed(t, indentedFlowTextFixture{paddingLeft: 20, text: errText}, 60, 10)

		lines := strings.Split(strings.TrimRight(rendered, "\n"), "\n")
		// First line of FlowText should have 20 leading spaces.
		if len(lines) < 2 {
			t.Fatalf("expected at least 2 lines, got:\n%s", rendered)
		}
		if !strings.HasPrefix(lines[0], strings.Repeat(" ", 20)) {
			t.Fatalf("expected line 0 to have 20 padding spaces, got %q", lines[0])
		}
		if !strings.Contains(rendered, "validation failed") {
			t.Fatalf("missing text in indented render:\n%s", rendered)
		}
	})
}

func TestFlowTextMajorCompositions(t *testing.T) {
	t.Parallel()

	t.Run("ConfirmActionRow failure text wraps losslessly", func(t *testing.T) {
		t.Parallel()

		longErr := "workspace deletion failed because underlying storage is locked by active process PID 49204"
		row := NewConfirmActionRow("del", ConfirmActionCopy{
			Label:        "delete",
			IdleValue:    "workspace",
			IdleAction:   "delete ↵",
			FailedValue:  "failed",
			FailedAction: "retry ↵",
		}, func() error {
			return errors.New(longErr)
		})
		row.OpenConfirm()
		row.Confirm()

		rendered := testkit.RenderMountedTrimmed(t, row, 50, 10)
		if !strings.Contains(rendered, "failed") || !strings.Contains(rendered, "retry ↵") {
			t.Fatalf("ConfirmActionRow header corrupted:\n%s", rendered)
		}
		words := strings.Fields(longErr)
		for _, w := range words {
			if !strings.Contains(rendered, w) {
				t.Fatalf("ConfirmActionRow failure subtext missing word %q:\n%s", w, rendered)
			}
		}
		lines := countNonEmptyLines(rendered)
		if lines < 2 {
			t.Fatalf("expected multi-line layout for wrapped error subtext, got %d lines:\n%s", lines, rendered)
		}
	})

	t.Run("Select detail text wraps losslessly", func(t *testing.T) {
		t.Parallel()

		longDetail := "Select a cloud provider to configure authentication, upstream model routing, and workspace endpoint credentials."
		sel := NewSelect(SelectProps{
			ID:     "test-select",
			Label:  "provider",
			Value:  "aws-bedrock",
			Detail: longDetail,
		})

		rendered := testkit.RenderMountedTrimmed(t, sel, 60, 10)
		if !strings.Contains(rendered, "provider") || !strings.Contains(rendered, "aws-bedrock") {
			t.Fatalf("Select row corrupted:\n%s", rendered)
		}
		words := strings.Fields(longDetail)
		for _, w := range words {
			if !strings.Contains(rendered, w) {
				t.Fatalf("Select detail missing word %q:\n%s", w, rendered)
			}
		}
		lines := countNonEmptyLines(rendered)
		if lines < 2 {
			t.Fatalf("expected multi-line layout for wrapped Select detail, got %d lines:\n%s", lines, rendered)
		}
	})
}

func countNonEmptyLines(s string) int {
	count := 0
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			count++
		}
	}
	return count
}
