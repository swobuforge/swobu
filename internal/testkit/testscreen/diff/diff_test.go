package diff

import (
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/testkit/testscreen"
)

func TestCompareDetectsStyleOnlyChange(t *testing.T) {
	expected := screenFromText(t, "fallback 1")
	actual := copyScreen(expected)
	for x := 0; x < len(actual.Rows[0]); x++ {
		actual.Rows[0][x].Rendition = testscreen.Rendition{FG: testscreen.ANSIColor(208), Attrs: testscreen.AttrBold}
	}
	res, err := Compare(expected, actual, 1, 1)
	if err == nil {
		t.Fatal("style-only change passed")
	}
	for _, want := range []string{"line 1, columns 1–10", "expected: default", "actual:   ansi(208)", "actual:   bold"} {
		if !strings.Contains(res.Diff, want) {
			t.Fatalf("diff missing %q:\n%s", want, res.Diff)
		}
	}
}

func TestCompareDetectsEachTerminalDimension(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testscreen.Cell)
		want   string
	}{
		{"text", func(c *testscreen.Cell) { c.Text = "y" }, "expected: x"},
		{"width", func(c *testscreen.Cell) { c.Width = 2 }, "actual:   2"},
		{"foreground", func(c *testscreen.Cell) { c.Rendition.FG = testscreen.ANSIColor(2) }, "actual:   ansi(2)"},
		{"background", func(c *testscreen.Cell) { c.Rendition.BG = testscreen.RGBColor(1, 2, 3) }, "actual:   rgb(1,2,3)"},
		{"attributes", func(c *testscreen.Cell) { c.Rendition.Attrs = testscreen.AttrReverse }, "actual:   reverse"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			expected := screenFromText(t, "x")
			actual := copyScreen(expected)
			test.mutate(&actual.Rows[0][0])
			result, err := Compare(expected, actual, 1, 1)
			if err == nil || !strings.Contains(result.Diff, test.want) {
				t.Fatalf("diff = %q, err = %v; want %q", result.Diff, err, test.want)
			}
		})
	}
}

func TestCompareDoesNotCoalesceDifferentMismatchKinds(t *testing.T) {
	expected := screenFromText(t, "ab")
	actual := copyScreen(expected)
	actual.Rows[0][0].Rendition.FG = testscreen.ANSIColor(1)
	actual.Rows[0][1].Rendition.BG = testscreen.ANSIColor(2)
	result, err := Compare(expected, actual, 2, 1)
	if err == nil {
		t.Fatal("style changes passed")
	}
	if !strings.Contains(result.Diff, "columns 1–1") {
		t.Fatalf("different mismatch kinds were coalesced:\n%s", result.Diff)
	}
}

func TestCompareDoesNotCoalesceDifferentStyleSignatures(t *testing.T) {
	expected := screenFromText(t, "ab")
	actual := copyScreen(expected)
	actual.Rows[0][0].Rendition.FG = testscreen.ANSIColor(1)
	actual.Rows[0][1].Rendition.FG = testscreen.ANSIColor(2)
	result, err := Compare(expected, actual, 2, 1)
	if err == nil {
		t.Fatal("style changes passed")
	}
	if !strings.Contains(result.Diff, "columns 1–1") {
		t.Fatalf("different style signatures were coalesced:\n%s", result.Diff)
	}
}

func TestCompareExactViewportRejectsAdditionalBlankArea(t *testing.T) {
	expected := screenFromText(t, "x").Viewport(2, 1)
	actual := expected.Viewport(3, 2)
	result, err := CompareViewport(expected, actual, 2, 1, ExactViewport)
	if err == nil || !strings.Contains(result.Diff, "actual viewport is 3x2, want exact 2x1") {
		t.Fatalf("exact viewport result = %#v, err = %v", result, err)
	}
}

func TestCompareDetectsStyledSpace(t *testing.T) {
	expected := screenFromText(t, " ")
	actual := copyScreen(expected)
	actual.Rows[0][0].Rendition.FG = testscreen.ANSIColor(1)
	result, err := Compare(expected, actual, 1, 1)
	if err == nil || !strings.Contains(result.Diff, "actual:   ansi(1)") {
		t.Fatalf("styled-space diff = %q, err = %v", result.Diff, err)
	}
}

func copyScreen(screen testscreen.Screen) testscreen.Screen {
	out := testscreen.New(screen.Width, len(screen.Rows))
	for y := range screen.Rows {
		copy(out.Rows[y], screen.Rows[y])
	}
	return out
}

func screenFromText(t testing.TB, text string) testscreen.Screen {
	t.Helper()
	screen, err := testscreen.ParseANSI([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	return screen
}
