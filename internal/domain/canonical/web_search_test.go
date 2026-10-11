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

func TestWebSearchAccessModeIsCanonicalDeclarationIdentity(t *testing.T) {
	cached, err := NewWebSearchDeclarationWithAccess(WebSearchAccessCached)
	if err != nil {
		t.Fatal(err)
	}
	clone := cached.Clone()
	if mode, ok := clone.WebSearchAccessMode(); !ok || mode != WebSearchAccessCached {
		t.Fatalf("cloned access mode = %v, %v", mode, ok)
	}
	if cached.Equivalent(NewWebSearchDeclaration()) {
		t.Fatal("cached and unrestricted declarations were equivalent")
	}
	if _, err := NewWebSearchDeclarationWithAccess(WebSearchAccessMode(255)); err == nil {
		t.Fatal("invalid access mode was accepted")
	}
}
