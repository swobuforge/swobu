package diff

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/swobuforge/swobu/testscreen/buf"
)

type CompareResult struct {
	Expected string
	Actual   string
	Diff     string
}

func ViewportForFixture(raw string, minCols, minRows int) (int, int) {
	lines := strings.Split(raw, "\n")
	cols := 1
	for _, line := range lines {
		if w := utf8.RuneCountInString(line); w > cols {
			cols = w
		}
	}
	rows := len(lines)
	if rows < 1 {
		rows = 1
	}
	if minCols > 0 && cols < minCols {
		cols = minCols
	}
	if minRows > 0 && rows < minRows {
		rows = minRows
	}
	return cols, rows
}

func CompareStrings(expectedRaw, actualRaw string, minCols, minRows int, normalize func(string) string) (CompareResult, error) {
	cols, rows := ViewportForFixture(expectedRaw, minCols, minRows)
	expected := buf.FromStringWithViewport(expectedRaw, cols, rows).String()
	actual := buf.FromStringWithViewport(actualRaw, cols, rows).String()
	if normalize != nil {
		expected = normalize(expected)
		actual = normalize(actual)
	}
	if expected == actual {
		return CompareResult{Expected: expected, Actual: actual}, nil
	}
	return CompareResult{
		Expected: expected,
		Actual:   actual,
		Diff:     literalLineDiff(expected, actual),
	}, fmt.Errorf("visual mismatch")
}

func literalLineDiff(expected, actual string) string {
	expectedLines := strings.Split(expected, "\n")
	actualLines := strings.Split(actual, "\n")
	max := len(expectedLines)
	if len(actualLines) > max {
		max = len(actualLines)
	}
	var out []string
	for i := 0; i < max; i++ {
		e := ""
		a := ""
		if i < len(expectedLines) {
			e = expectedLines[i]
		}
		if i < len(actualLines) {
			a = actualLines[i]
		}
		if e == a {
			continue
		}
		out = append(out,
			fmt.Sprintf("line %d", i+1),
			"  expected: "+e,
			"  actual  : "+a,
		)
	}
	return strings.Join(out, "\n")
}
