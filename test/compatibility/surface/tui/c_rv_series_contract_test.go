package tui_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCSeries_FixturesCarryClientGrammar(t *testing.T) {
	fixtures := fixturesByPrefix(t, "C-")
	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			text := mustReadWireframeFixture(t, name)
			mustContain(t, strings.ToLower(text), "clients")
			mustContain(t, text, "Swobu")
		})
	}
}

func TestRVSeries_FixturesCarryRoutingValidationGrammar(t *testing.T) {
	fixtures := fixturesByPrefix(t, "RV-")
	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			text := strings.ToLower(mustReadWireframeFixture(t, name))
			mustContain(t, text, "routing")
		})
	}
}

func fixturesByPrefix(t *testing.T, prefix string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join("testdata", "wireframes", "*", prefix+"*.txt"))
	if err != nil {
		t.Fatalf("glob fixtures for %q: %v", prefix, err)
	}
	if len(matches) == 0 {
		t.Fatalf("no fixtures found for prefix %q", prefix)
	}
	out := make([]string, 0, len(matches))
	for _, path := range matches {
		out = append(out, filepath.Base(path))
	}
	return out
}

func mustReadWireframeFixture(t *testing.T, fixture string) string {
	t.Helper()
	path := mustResolveWireframeFixturePath(t, fixture)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %q: %v", path, err)
	}
	return string(raw)
}
