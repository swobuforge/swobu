package tui_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestFixtures_ExistAndHaveCoreShellShape(t *testing.T) {
	allNames := allFixtureBasenames(t)
	for _, name := range allNames {
		t.Run(name, func(t *testing.T) {
			fixturePath := mustResolveWireframeFixturePath(t, name)
			content, err := os.ReadFile(fixturePath)
			if err != nil {
				t.Fatalf("read fixture %q: %v", fixturePath, err)
			}
			text := string(content)
			if strings.TrimSpace(text) == "" {
				t.Fatalf("fixture %q is empty", fixturePath)
			}
			mustContain(t, text, "Swobu")
			if !strings.Contains(text, "--------------------------------------------------") &&
				!strings.Contains(text, "──────────────────────────────────────────────────") {
				t.Fatalf("fixture %q is missing a horizontal separator", name)
			}
			hasNavFooter := strings.Contains(text, "↑↓ move") ||
				strings.Contains(text, "busy") ||
				strings.Contains(text, "↵ select") ||
				strings.Contains(text, "↵ save") ||
				strings.Contains(text, "↵ edit") ||
				strings.Contains(text, "edit ↵")
			// Some dense list viewports legitimately consume the bottom row and
			// expose operator affordance verbs inline on rows instead of a footer.
			hasInlineAffordance := strings.Contains(text, "open ↵") ||
				strings.Contains(text, "close ↵") ||
				strings.Contains(text, "browse ↵") ||
				strings.Contains(text, "choose ↵")
			if !hasNavFooter && !hasInlineAffordance {
				t.Fatalf("fixture %q is missing recognizable operator affordance text", name)
			}
		})
	}
}

func TestFixtures_AllWireframeArtifactsAreListed(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "wireframes", "*", "*.txt"))
	if err != nil {
		t.Fatalf("glob wireframes: %v", err)
	}
	want := make([]string, 0, len(files))
	for _, file := range files {
		want = append(want, filepath.Base(file))
	}
	got := allFixtureBasenames(t)
	slices.Sort(want)
	slices.Sort(got)
	if len(want) != len(got) {
		t.Fatalf("wireframe manifest length mismatch: got=%d want=%d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("wireframe manifest mismatch at %d: got=%q want=%q", i, got[i], want[i])
		}
	}
}

func allFixtureBasenames(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("testdata", "wireframes", "*", "*.txt"))
	if err != nil {
		t.Fatalf("glob wireframes: %v", err)
	}
	out := make([]string, 0, len(files))
	for _, file := range files {
		out = append(out, filepath.Base(file))
	}
	return out
}

func mustContain(t *testing.T, text string, needle string) {
	t.Helper()
	if !strings.Contains(text, needle) {
		t.Fatalf("missing %q in fixture: %q", needle, text)
	}
}
