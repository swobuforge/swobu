package tui_test

import "testing"

func TestWireframeLineMatchesPattern_Exact(t *testing.T) {
	tests := []struct {
		name     string
		expected string
		actual   string
		match    bool
	}{
		{
			name:     "Exact",
			expected: "run on            OpenRouter                                choose ↵",
			actual:   "run on            OpenRouter                                choose ↵",
			match:    true,
		},
		{
			name:     "DifferentCellDoesNotMatch",
			expected: "run on            XpenRouter                                choose ↵",
			actual:   "run on            OpenRouter                                choose ↵",
			match:    false,
		},
		{
			name:     "WildcardDotMatchesSingleCell",
			expected: "run on            .penRouter                                choose ↵",
			actual:   "run on            OpenRouter                                choose ↵",
			match:    true,
		},
		{
			name:     "ExactDotsMatch",
			expected: "abc...xyz",
			actual:   "abc...xyz",
			match:    true,
		},
		{
			name:     "DifferentWidthDoesNotMatch",
			expected: "abc...xyz",
			actual:   "abc12xyz",
			match:    false,
		},
		{
			name:     "BackslashIsLiteralCharacter",
			expected: "openrouter\\.ai",
			actual:   "openrouter.ai",
			match:    false,
		},
		{
			name:     "BackslashLiteralMatch",
			expected: "openrouter\\.ai",
			actual:   "openrouter\\.ai",
			match:    true,
		},
		{
			name:     "DotWildcardCoversChangingPortCells",
			expected: "http://127.0.0.1:...../c/acme/",
			actual:   "http://127.0.0.1:77777/c/acme/",
			match:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := wireframeLineMatchesPattern(tt.expected, tt.actual)
			if got != tt.match {
				t.Fatalf("wireframeLineMatchesPattern(%q, %q)=%t want=%t", tt.expected, tt.actual, got, tt.match)
			}
		})
	}
}

func TestRenderTerminalMatrixString_PreservesSpacingAndRows(t *testing.T) {
	got := renderTerminalMatrixString("a  b\nx", 5, 2)
	want := "a  b \nx    "
	if got != want {
		t.Fatalf("renderTerminalMatrixString()=%q want=%q", got, want)
	}
}

func TestFixtureViewportFromText_ClampsToMinimumViewport(t *testing.T) {
	cols, rows := fixtureViewportFromText("short\nfixture")
	if cols != minTerminalCols || rows != minTerminalRows {
		t.Fatalf("fixtureViewportFromText()=%dx%d want=%dx%d", cols, rows, minTerminalCols, minTerminalRows)
	}
}

func TestFixtureViewportFromText_PreservesLargerViewport(t *testing.T) {
	wide := "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
	raw := wide + "\n" + wide
	cols, rows := fixtureViewportFromText(raw)
	if cols <= minTerminalCols {
		t.Fatalf("fixtureViewportFromText() cols=%d, want >%d", cols, minTerminalCols)
	}
	if rows != minTerminalRows {
		t.Fatalf("fixtureViewportFromText() rows=%d, want %d", rows, minTerminalRows)
	}
}
