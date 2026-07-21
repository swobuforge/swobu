package canonical

import "testing"

func TestWebSearchDeclarationHasFixedIdentityAndNoState(t *testing.T) {
	declaration := NewWebSearchDeclaration()
	if declaration.Kind() != ToolKindWebSearch || declaration.Key() != WebSearchToolKey() {
		t.Fatalf("web search identity = (%q, %q)", declaration.Kind(), declaration.Key())
	}
	if declaration.Kind() != ToolKindWebSearch {
		t.Fatal("web-search declaration lost its sole branch")
	}
	if clone := declaration.Clone(); clone.Key() != WebSearchToolKey() {
		t.Fatalf("clone key = %q", clone.Key())
	}
}

func TestToolSetRejectsSecondWebSearchDeclaration(t *testing.T) {
	if _, err := NewToolSet([]ToolDeclaration{NewWebSearchDeclaration(), NewWebSearchDeclaration()}); err == nil {
		t.Fatal("tool set accepted a second fixed-key web-search declaration")
	}
}
