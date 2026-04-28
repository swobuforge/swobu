package tui_test

import (
	"strings"
	"testing"
)

func TestCRSeries_FixturesCarryRoutingCredentialGrammar(t *testing.T) {
	for _, name := range fixturesByPrefix(t, "CR-") {
		t.Run(name, func(t *testing.T) {
			text := strings.ToLower(mustReadWireframeFixture(t, name))
			mustContain(t, text, "routing")
			mustContain(t, text, "run on")
		})
	}
}

func TestCRSeries_ScopedPickerAndScrollVocabulary(t *testing.T) {
	mustIncludeAny(t, "CR-03_provider_picker_top.txt", []string{"OpenAI", "OpenRouter"})
	mustIncludeAny(t, "CR-04_provider_picker_scrolled.txt", []string{"run on"})
	mustIncludeAny(t, "CR-09_model_picker_top.txt", []string{"model"})
	mustIncludeAny(t, "CR-10_model_picker_scrolled.txt", []string{"model"})
}

func mustIncludeAny(t *testing.T, fixture string, snippets []string) {
	t.Helper()
	text := mustReadWireframeFixture(t, fixture)
	for _, snippet := range snippets {
		if !strings.Contains(text, snippet) {
			t.Fatalf("fixture %q missing %q", fixture, snippet)
		}
	}
}
