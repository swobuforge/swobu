package tui_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

const updateWireframesEnv = "SWOBU_UPDATE_WIREFRAMES"
const updateScreenshotsLegacyEnv = "SWOBU_UPDATE_SCREENSHOTS"
const wireframeFixtureDir = "testdata/wireframes"
const minTerminalCols = 60
const minTerminalRows = 18

var (
	wireframeClockPattern      = regexp.MustCompile(`\b\d{2}:\d{2}:\d{2}\b`)
	wireframeRequestIDPattern  = regexp.MustCompile(`\b[0-9a-f]{32}\b`)
	wireframeResultPattern     = regexp.MustCompile(`\b(done|429)\b`)
	wireframeDotsResultPattern = regexp.MustCompile(`\s\.{3,4}\s`)
	wireframeBackendError      = regexp.MustCompile(`backend error \.{3,4}`)
)

func assertWireframeEqualsFixture(t *testing.T, visible string, fixtureName string) {
	t.Helper()
	fixturePath := mustResolveWireframeFixturePath(t, fixtureName)
	if shouldUpdateWireframeFixtures() {
		if err := os.MkdirAll(filepath.Dir(fixturePath), 0o755); err != nil {
			t.Fatalf("create fixture dir for %q: %v", fixturePath, err)
		}
		if err := os.WriteFile(fixturePath, []byte(visible), 0o644); err != nil {
			t.Fatalf("update wireframe fixture %q: %v", fixturePath, err)
		}
		return
	}
	expected, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read wireframe fixture %q: %v", fixturePath, err)
	}
	cols, rows := fixtureViewportFromText(string(expected))
	expectedText := renderTerminalMatrixString(string(expected), cols, rows)
	actualText := renderTerminalMatrixString(visible, cols, rows)
	expectedText = normalizeWireframeDynamicValues(expectedText)
	actualText = normalizeWireframeDynamicValues(actualText)
	if wireframeContainsAllExpected(expectedText, actualText) {
		return
	}
	diff := literalLineDiff(expectedText, actualText)
	artifactDir := t.TempDir()
	expectedArtifact := filepath.Join(artifactDir, "expected.txt")
	actualArtifact := filepath.Join(artifactDir, "actual.txt")
	diffArtifact := filepath.Join(artifactDir, "diff.txt")
	_ = os.WriteFile(expectedArtifact, []byte(expectedText), 0o644)
	_ = os.WriteFile(actualArtifact, []byte(actualText), 0o644)
	_ = os.WriteFile(diffArtifact, []byte(diff), 0o644)
	t.Fatalf(
		"literal wireframe mismatch fixture=%q\nartifacts: expected=%s actual=%s diff=%s\n%s",
		fixturePath, expectedArtifact, actualArtifact, diffArtifact, diff,
	)
}

func normalizeWireframeDynamicValues(text string) string {
	if strings.TrimSpace(text) == "" {
		return text
	}
	text = wireframeClockPattern.ReplaceAllString(text, "........")
	text = wireframeRequestIDPattern.ReplaceAllString(text, "................................")
	text = wireframeResultPattern.ReplaceAllStringFunc(text, func(token string) string {
		return strings.Repeat(".", len(token))
	})
	text = wireframeDotsResultPattern.ReplaceAllString(text, " .... ")
	text = wireframeBackendError.ReplaceAllString(text, "....")
	return text
}

func mustResolveWireframeFixturePath(t *testing.T, fixtureName string) string {
	t.Helper()
	needle := strings.TrimSpace(fixtureName)
	if needle == "" {
		t.Fatalf("empty fixture name")
	}
	candidate := filepath.Join(wireframeFixtureDir, needle)
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	var matches []string
	err := filepath.WalkDir(wireframeFixtureDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.EqualFold(d.Name(), needle) {
			matches = append(matches, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk fixture dir: %v", err)
	}
	if len(matches) == 1 {
		return matches[0]
	}
	if len(matches) > 1 {
		t.Fatalf("fixture name %q is ambiguous; matches=%v", needle, matches)
	}
	t.Fatalf("fixture %q not found in %q", needle, wireframeFixtureDir)
	return ""
}

func fixtureViewportFromText(raw string) (cols int, rows int) {
	lines := strings.Split(raw, "\n")
	cols = 1
	for _, line := range lines {
		if w := utf8.RuneCountInString(line); w > cols {
			cols = w
		}
	}
	rows = len(lines)
	if rows < 1 {
		rows = 1
	}
	if cols < minTerminalCols {
		cols = minTerminalCols
	}
	if rows < minTerminalRows {
		rows = minTerminalRows
	}
	return cols, rows
}

func renderTerminalMatrixString(text string, cols int, rows int) string {
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	grid := make([][]rune, rows)
	for y := 0; y < rows; y++ {
		grid[y] = make([]rune, cols)
		for x := 0; x < cols; x++ {
			grid[y][x] = ' '
		}
	}
	x := 0
	y := 0
	for _, r := range text {
		switch r {
		case '\r':
			x = 0
		case '\n':
			y++
			x = 0
		default:
			if y >= rows {
				continue
			}
			if x < cols {
				grid[y][x] = r
			}
			x++
		}
		if y >= rows {
			break
		}
	}
	lines := make([]string, rows)
	for i := 0; i < rows; i++ {
		lines[i] = string(grid[i])
	}
	return strings.Join(lines, "\n")
}

func wireframeContainsAllExpected(expected string, actual string) bool {
	expectedLines := strings.Split(expected, "\n")
	actualLines := strings.Split(actual, "\n")
	if len(expectedLines) != len(actualLines) {
		return false
	}
	for i := range expectedLines {
		if !wireframeLineMatchesPattern(expectedLines[i], actualLines[i]) {
			return false
		}
	}
	return true
}

func wireframeLineMatchesPattern(expectedLine, actualLine string) bool {
	if dividerLine(expectedLine) && dividerLine(actualLine) {
		return true
	}
	expected := []rune(expectedLine)
	actual := []rune(actualLine)
	i := 0
	j := 0
	for i < len(expected) && j < len(actual) {
		if expected[i] == '.' {
			i++
			j++
			continue
		}
		if unicode.IsSpace(expected[i]) {
			if !unicode.IsSpace(actual[j]) {
				return false
			}
			for i < len(expected) && unicode.IsSpace(expected[i]) {
				i++
			}
			for j < len(actual) && unicode.IsSpace(actual[j]) {
				j++
			}
			continue
		}
		if expected[i] != actual[j] {
			return false
		}
		i++
		j++
	}
	for i < len(expected) && unicode.IsSpace(expected[i]) {
		i++
	}
	for j < len(actual) && unicode.IsSpace(actual[j]) {
		j++
	}
	return i == len(expected) && j == len(actual)
}

func dividerLine(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	for _, r := range trimmed {
		if r != '─' && r != '-' && r != '.' {
			return false
		}
	}
	return true
}

func shouldUpdateWireframeFixtures() bool {
	return envIsTrue(updateWireframesEnv) || envIsTrue(updateScreenshotsLegacyEnv)
}

func envIsTrue(key string) bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv(key)), "1")
}

func literalLineDiff(expected string, actual string) string {
	exp := strings.Split(expected, "\n")
	act := strings.Split(actual, "\n")
	maxLines := len(exp)
	if len(act) > maxLines {
		maxLines = len(act)
	}
	var b strings.Builder
	changes := 0
	for i := 0; i < maxLines; i++ {
		e := lineAt(exp, i)
		a := lineAt(act, i)
		if wireframeLineMatchesPattern(e, a) {
			continue
		}
		changes++
		fmt.Fprintf(&b, "L%03d - %s\n", i+1, e)
		fmt.Fprintf(&b, "L%03d + %s\n", i+1, a)
		if changes >= 120 {
			b.WriteString("... diff truncated after 120 changed lines\n")
			break
		}
	}
	if changes == 0 {
		return ""
	}
	return b.String()
}

func lineAt(lines []string, idx int) string {
	if idx < 0 || idx >= len(lines) {
		return "<missing>"
	}
	return lines[idx]
}
