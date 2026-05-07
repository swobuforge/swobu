package clientprofile

import (
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/compatibility"
)

func TestCatalog_ContainsCanonicalProfiles(t *testing.T) {
	t.Parallel()

	profiles := Catalog()
	if len(profiles) != 6 {
		t.Fatalf("profile count=%d want 6", len(profiles))
	}
	for _, id := range []string{
		"codex",
		"claude",
		"aider",
		"continue",
		"opencode",
		"other",
	} {
		if profile := FindByID(profiles, id); profile == nil {
			t.Fatalf("missing profile %q", id)
		}
	}
}

func TestProfileActions_ParetoMatrix(t *testing.T) {
	t.Parallel()

	baseURL := "http://127.0.0.1:7926/c/acme/"
	openAIBase := "http://127.0.0.1:7926/c/acme/v1"

	codex := FindByID(Catalog(), "codex")
	codexActions := codex.Actions(baseURL)
	if len(codexActions) != 2 {
		t.Fatalf("codex actions=%d want 2", len(codexActions))
	}
	if got := codexActions[0].RowLabel(); got != "file config" {
		t.Fatalf("codex primary=%q", got)
	}
	if got := codexActions[1].RowLabel(); got != "run" {
		t.Fatalf("codex run=%q", got)
	}

	claude := FindByID(Catalog(), "claude")
	claudeActions := claude.Actions(baseURL)
	if len(claudeActions) != 2 {
		t.Fatalf("claude actions=%d want 2", len(claudeActions))
	}
	if got := claudeActions[0].RowLabel(); got != "run" {
		t.Fatalf("claude primary=%q", got)
	}
	if got := claudeActions[1].RowLabel(); got != "environment values" {
		t.Fatalf("claude env row=%q", got)
	}

	aider := FindByID(Catalog(), "aider")
	aiderActions := aider.Actions(baseURL)
	if len(aiderActions) != 3 {
		t.Fatalf("aider actions=%d want 3", len(aiderActions))
	}
	if got := aiderActions[0].RowLabel(); got != "file config" {
		t.Fatalf("aider primary=%q", got)
	}
	if got := aiderActions[2].RowLabel(); got != "environment values" {
		t.Fatalf("aider env row=%q", got)
	}
	if !strings.Contains(aiderActions[0].Content, "openai-api-base: "+openAIBase) {
		t.Fatalf("aider file config=%q", aiderActions[0].Content)
	}
	if !strings.Contains(aiderActions[2].Content, "OPENAI_API_KEY=swobu-placeholder") {
		t.Fatalf("aider env values=%q", aiderActions[2].Content)
	}

	continueProfile := FindByID(Catalog(), "continue")
	continueActions := continueProfile.Actions(baseURL)
	if len(continueActions) != 2 {
		t.Fatalf("continue actions=%d want 2", len(continueActions))
	}
	if got := continueActions[0].RowLabel(); got != "file config" {
		t.Fatalf("continue primary=%q", got)
	}
	if !strings.Contains(continueActions[0].Content, "apiBase: "+openAIBase) {
		t.Fatalf("continue file config=%q", continueActions[0].Content)
	}
	if got := continueActions[1].RowLabel(); got != "run" {
		t.Fatalf("continue run=%q", got)
	}

	openCode := FindByID(Catalog(), "opencode")
	openCodeActions := openCode.Actions(baseURL)
	if len(openCodeActions) != 2 {
		t.Fatalf("opencode actions=%d want 2", len(openCodeActions))
	}
	if got := openCodeActions[0].RowLabel(); got != "file config" {
		t.Fatalf("opencode primary=%q", got)
	}
	if !strings.Contains(openCodeActions[0].Content, `"model": "swobu/`+compatibility.PrimaryTargetSelector+`"`) {
		t.Fatalf("opencode file config=%q", openCodeActions[0].Content)
	}
	if got := openCodeActions[1].RowLabel(); got != "run" {
		t.Fatalf("opencode run=%q", got)
	}

	other := FindByID(Catalog(), "other")
	otherActions := other.Actions(baseURL)
	if len(otherActions) != 2 {
		t.Fatalf("other actions=%d want 2", len(otherActions))
	}
	if got := otherActions[0].RowLabel(); got != "open" {
		t.Fatalf("other primary=%q", got)
	}
	if got := otherActions[1].RowLabel(); got != "copy values" {
		t.Fatalf("other copy=%q", got)
	}
	if !strings.Contains(otherActions[0].ActionSummary(), "openai + anthropic") {
		t.Fatalf("other summary=%q", otherActions[0].ActionSummary())
	}
	if got := otherActions[1].Content; got != "Base URL: http://127.0.0.1:7926/c/acme/\nModel:    primary" {
		t.Fatalf("other copy values=%q", got)
	}
}

func TestCatalog_RunVerbOwnershipBoundary(t *testing.T) {
	t.Parallel()

	baseURL := "http://127.0.0.1:7926/c/acme/"
	allowedRunProfiles := map[string]struct{}{
		"aider":    {},
		"codex":    {},
		"claude":   {},
		"continue": {},
		"opencode": {},
	}
	for _, profile := range Catalog() {
		id := profile.Identity().ID
		actions := profile.Actions(baseURL)
		for _, action := range actions {
			if !action.IsRunAction() {
				continue
			}
			if _, ok := allowedRunProfiles[id]; !ok {
				t.Fatalf("profile %q must not expose run action outside the client run-capability ladder; action=%q", id, action.RowLabel())
			}
		}
	}
}

func TestCatalog_ClientRunCapabilityLadder(t *testing.T) {
	t.Parallel()

	baseURL := "http://127.0.0.1:7926/c/acme/"
	expectRunRows := map[string]struct {
		label string
		verb  string
	}{
		"aider":    {label: "run", verb: "run"},
		"codex":    {label: "run", verb: "run"},
		"claude":   {label: "run", verb: "run"},
		"continue": {label: "run", verb: "run"},
		"opencode": {label: "run", verb: "run"},
	}
	for profileID, want := range expectRunRows {
		profile := FindByID(Catalog(), profileID)
		if profile == nil {
			t.Fatalf("missing profile %q", profileID)
		}
		actions := profile.Actions(baseURL)
		found := false
		for _, action := range actions {
			if action.RowLabel() != want.label {
				continue
			}
			found = true
			if got := action.ActionVerb(); got != want.verb {
				t.Fatalf("profile %q row %q verb=%q want=%q", profileID, want.label, got, want.verb)
			}
		}
		if !found {
			t.Fatalf("profile %q missing expected run row label %q", profileID, want.label)
		}
	}
}

func TestCatalog_RunRowsUseVerifiedLanguage(t *testing.T) {
	t.Parallel()

	baseURL := "http://127.0.0.1:7926/c/acme/"
	for _, profile := range Catalog() {
		for _, action := range profile.Actions(baseURL) {
			label := strings.ToLower(strings.TrimSpace(action.RowLabel()))
			if label != "run" {
				continue
			}
			if got := action.ActionSummary(); got != "command" {
				t.Fatalf("profile %q run row %q summary=%q want=%q", profile.Identity().ID, action.RowLabel(), got, "command")
			}
		}
	}
}

func TestCatalog_NoCopyOnlyRunRows(t *testing.T) {
	t.Parallel()

	baseURL := "http://127.0.0.1:7926/c/acme/"
	for _, profile := range Catalog() {
		actions := profile.Actions(baseURL)
		for _, action := range actions {
			if strings.TrimSpace(action.RowLabel()) != "run" {
				continue
			}
			if got := action.ActionVerb(); got == "copy" {
				t.Fatalf("profile %q run must not be copy-only once run wiring exists; summary=%q", profile.Identity().ID, action.ActionSummary())
			}
		}
	}
}

func TestCatalog_RunActionContentDerivedFromRunSpec(t *testing.T) {
	t.Parallel()

	baseURL := "http://127.0.0.1:7926/c/acme/"
	tests := []struct {
		profileID string
		contains  []string
	}{
		{
			profileID: "codex",
			contains:  []string{"codex", `model_provider="swobu"`},
		},
		{
			profileID: "claude",
			contains:  []string{"claude --model", "ANTHROPIC_BASE_URL=http://127.0.0.1:7926/c/acme/"},
		},
		{
			profileID: "aider",
			contains:  []string{"aider --model", "AIDER_OPENAI_API_BASE=http://127.0.0.1:7926/c/acme/v1"},
		},
		{
			profileID: "continue",
			contains:  []string{"cn --config ./swobu.continue.yaml", "Explain this codebase"},
		},
		{
			profileID: "opencode",
			contains:  []string{"opencode"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.profileID, func(t *testing.T) {
			t.Parallel()
			profile := FindByID(Catalog(), tc.profileID)
			if profile == nil {
				t.Fatalf("missing profile %q", tc.profileID)
			}
			actions := profile.Actions(baseURL)
			var run Action
			found := false
			for _, action := range actions {
				if strings.TrimSpace(action.RowLabel()) == "run" {
					run = action
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("missing run action for %q", tc.profileID)
			}
			for _, fragment := range tc.contains {
				if !strings.Contains(run.Content, fragment) {
					t.Fatalf("run content=%q missing fragment=%q", run.Content, fragment)
				}
			}
		})
	}
}
