package diff

import (
	"fmt"
	"strings"

	"github.com/swobuforge/swobu/internal/testkit/testscreen"
)

type CompareResult struct {
	Expected string
	Actual   string
	Diff     string
}

type ViewportMode uint8

const (
	MinViewport ViewportMode = iota
	ExactViewport
)

func Compare(expected, actual testscreen.Screen, minCols, minRows int) (CompareResult, error) {
	return CompareViewport(expected, actual, minCols, minRows, MinViewport)
}

func CompareViewport(expected, actual testscreen.Screen, cols, rows int, mode ViewportMode) (CompareResult, error) {
	if mode == ExactViewport {
		if expected.Width != cols || len(expected.Rows) != rows {
			return mismatch(expected.String(), actual.String(), fmt.Sprintf("fixture viewport is %dx%d, want exact %dx%d", expected.Width, len(expected.Rows), cols, rows))
		}
		if actual.Width != cols || len(actual.Rows) != rows {
			return mismatch(expected.String(), actual.String(), fmt.Sprintf("actual viewport is %dx%d, want exact %dx%d", actual.Width, len(actual.Rows), cols, rows))
		}
	} else {
		cols = max(expected.Width, actual.Width, cols, 1)
		rows = max(len(expected.Rows), len(actual.Rows), rows, 1)
	}
	expected = expected.Viewport(cols, rows)
	actual = actual.Viewport(cols, rows)
	expectedText, actualText := expected.String(), actual.String()
	if expectedText != actualText {
		return mismatch(expectedText, actualText, literalLineDiff(expectedText, actualText))
	}

	for y := 0; y < rows; y++ {
		for x := 0; x < cols; {
			e, a := expected.Cell(x, y), actual.Cell(x, y)
			if e == a {
				x++
				continue
			}
			start := x
			signature := differenceSignatureOf(e, a)
			for x < cols && differenceSignatureOf(expected.Cell(x, y), actual.Cell(x, y)) == signature {
				x++
			}
			return mismatch(expectedText, actualText, styleDiff(y, start, x-1, expected, actual))
		}
	}
	return CompareResult{Expected: expectedText, Actual: actualText}, nil
}

type differenceSignature struct {
	kind                       cellDifference
	expectedWidth, actualWidth int
	expected, actual           testscreen.Rendition
	expectedText, actualText   string
}

func differenceSignatureOf(expected, actual testscreen.Cell) differenceSignature {
	kind := differenceKind(expected, actual)
	signature := differenceSignature{kind: kind, expected: expected.Rendition, actual: actual.Rendition}
	if kind.width {
		signature.expectedWidth, signature.actualWidth = expected.Width, actual.Width
	}
	if kind.text {
		signature.expectedText, signature.actualText = expected.Text, actual.Text
	}
	return signature
}

type cellDifference struct {
	text, width, foreground, background, attributes bool
}

func differenceKind(expected, actual testscreen.Cell) cellDifference {
	return cellDifference{
		text:       expected.Text != actual.Text,
		width:      expected.Width != actual.Width,
		foreground: expected.Rendition.FG != actual.Rendition.FG,
		background: expected.Rendition.BG != actual.Rendition.BG,
		attributes: expected.Rendition.Attrs != actual.Rendition.Attrs,
	}
}

func mismatch(expected, actual, detail string) (CompareResult, error) {
	return CompareResult{Expected: expected, Actual: actual, Diff: detail}, fmt.Errorf("visual mismatch")
}

func styleDiff(y, start, end int, expected, actual testscreen.Screen) string {
	e, a := expected.Cell(start, y), actual.Cell(start, y)
	return fmt.Sprintf("line %d, columns %d–%d\n\ntext:\n  %q\n\nwidth:\n  expected: %d\n  actual:   %d\n\nforeground:\n  expected: %s\n  actual:   %s\n\nbackground:\n  expected: %s\n  actual:   %s\n\nattributes:\n  expected: %s\n  actual:   %s",
		y+1, start+1, end+1, cellRunText(actual, y, start, end), e.Width, a.Width,
		describeColor(e.Rendition.FG), describeColor(a.Rendition.FG), describeColor(e.Rendition.BG), describeColor(a.Rendition.BG),
		describeAttrs(e.Rendition.Attrs), describeAttrs(a.Rendition.Attrs))
}

func cellRunText(s testscreen.Screen, y, start, end int) string {
	var b strings.Builder
	for x := start; x <= end; x++ {
		if c := s.Cell(x, y); c.Width != 0 {
			b.WriteString(c.Text)
		}
	}
	return b.String()
}

func describeColor(c testscreen.Color) string {
	switch c.Kind {
	case testscreen.ColorIndexed:
		return fmt.Sprintf("ansi(%d)", c.Index)
	case testscreen.ColorRGB:
		return fmt.Sprintf("rgb(%d,%d,%d)", c.R, c.G, c.B)
	default:
		return "default"
	}
}

func describeAttrs(a testscreen.Attrs) string {
	if a == 0 {
		return "default"
	}
	parts := []string{}
	for _, item := range []struct {
		bit  testscreen.Attrs
		name string
	}{{testscreen.AttrBold, "bold"}, {testscreen.AttrReverse, "reverse"}} {
		if a&item.bit != 0 {
			parts = append(parts, item.name)
		}
	}
	return strings.Join(parts, ", ")
}

func literalLineDiff(expected, actual string) string {
	expectedLines, actualLines := strings.Split(expected, "\n"), strings.Split(actual, "\n")
	var out []string
	for i := 0; i < max(len(expectedLines), len(actualLines)); i++ {
		e, a := "", ""
		if i < len(expectedLines) {
			e = expectedLines[i]
		}
		if i < len(actualLines) {
			a = actualLines[i]
		}
		if e != a {
			out = append(out, fmt.Sprintf("line %d", i+1), "  expected: "+e, "  actual  : "+a)
		}
	}
	return strings.Join(out, "\n")
}
