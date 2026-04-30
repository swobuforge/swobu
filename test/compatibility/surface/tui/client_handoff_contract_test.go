package tui_test

import (
	"strings"
	"testing"

	"github.com/metrofun/swobu/internal/app/operator/clientprofile"
	"github.com/metrofun/swobu/internal/domain/compatibility"
	"github.com/metrofun/swobu/internal/terminalui/apps/cockpit/app/state/effect"
)

func TestClientHandoffProfiles_ContractPayloads(t *testing.T) {
	t.Parallel()

	baseURL := "http://127.0.0.1:7777/c/acme/"
	profiles := clientprofile.Catalog()
	if len(profiles) < 4 {
		t.Fatalf("profile count = %d, want >= 4", len(profiles))
	}

	codex := clientprofile.FindByID(profiles, "codex")
	if codex == nil {
		t.Fatal("missing codex profile")
	}
	codexActions := codex.Actions(baseURL)
	if len(codexActions) != 2 {
		t.Fatalf("codex actions = %d, want 2", len(codexActions))
	}
	if !strings.Contains(codexActions[0].Content, `model_provider = "swobu"`) {
		t.Fatalf("codex file config = %q", codexActions[0].Content)
	}
	if !strings.Contains(codexActions[0].Content, `base_url = "http://127.0.0.1:7777/c/acme/v1"`) {
		t.Fatalf("codex file config base_url = %q", codexActions[0].Content)
	}

	claude := clientprofile.FindByID(profiles, "claude")
	if claude == nil {
		t.Fatal("missing claude profile")
	}
	claudeActions := claude.Actions(baseURL)
	if len(claudeActions) != 2 {
		t.Fatalf("claude actions = %d, want 2", len(claudeActions))
	}
	if got := claudeActions[0].RowLabel(); got != "run" {
		t.Fatalf("claude primary row = %q", got)
	}
	if got := claudeActions[1].Content; got != "ANTHROPIC_BASE_URL="+baseURL+"\n"+"ANTHROPIC_MODEL="+compatibility.PrimaryTargetSelector {
		t.Fatalf("claude env copy = %q", got)
	}

	continueProfile := clientprofile.FindByID(profiles, "continue")
	if continueProfile == nil {
		t.Fatal("missing continue profile")
	}
	continueActions := continueProfile.Actions(baseURL)
	if len(continueActions) != 2 {
		t.Fatalf("continue actions = %d, want 2", len(continueActions))
	}
	if !strings.Contains(continueActions[0].Content, "apiBase: http://127.0.0.1:7777/c/acme/v1") {
		t.Fatalf("continue file config missing apiBase /v1: %q", continueActions[0].Content)
	}
	if got := continueActions[1].RowLabel(); got != "run" {
		t.Fatalf("continue run row = %q", got)
	}

	openCode := clientprofile.FindByID(profiles, "opencode")
	if openCode == nil {
		t.Fatal("missing opencode profile")
	}
	openCodeActions := openCode.Actions(baseURL)
	if len(openCodeActions) != 2 {
		t.Fatalf("opencode actions = %d, want 2", len(openCodeActions))
	}
	if got := openCodeActions[1].RowLabel(); got != "run" {
		t.Fatalf("opencode run row = %q", got)
	}
	if !strings.Contains(openCodeActions[1].Content, "opencode -p") || !strings.Contains(openCodeActions[1].Content, "Explain this codebase") {
		t.Fatalf("opencode run body = %q", openCodeActions[1].Content)
	}
}

func TestClientRunDisplayCommands_Contract(t *testing.T) {
	baseURL := "http://127.0.0.1:7777/c/acme/"

	aider, ok := effect.RunClientDisplayCommand("aider", baseURL, "")
	if !ok {
		t.Fatal("missing aider display command")
	}
	if want := "AIDER_OPENAI_API_BASE=http://127.0.0.1:7777/c/acme/v1 aider --model openai/" + compatibility.PrimaryTargetSelector; aider != want {
		t.Fatalf("aider display command = %q want %q", aider, want)
	}

	codex, ok := effect.RunClientDisplayCommand("codex", baseURL, "")
	if !ok {
		t.Fatal("missing codex display command")
	}
	if want := `codex -c model="` + compatibility.PrimaryTargetSelector + `" -c model_provider="swobu" -c model_providers.swobu.name="Swobu" -c model_providers.swobu.base_url="http://127.0.0.1:7777/c/acme/v1" -c forced_login_method="api"`; codex != want {
		t.Fatalf("codex display command = %q want %q", codex, want)
	}

	claude, ok := effect.RunClientDisplayCommand("claude", baseURL, "")
	if !ok {
		t.Fatal("missing claude display command")
	}
	if want := "ANTHROPIC_BASE_URL=http://127.0.0.1:7777/c/acme/ ANTHROPIC_MODEL=" + compatibility.PrimaryTargetSelector + " claude --model " + compatibility.PrimaryTargetSelector; claude != want {
		t.Fatalf("claude display command = %q want %q", claude, want)
	}

	continueCmd, ok := effect.RunClientDisplayCommand("continue", baseURL, "")
	if !ok {
		t.Fatal("missing continue display command")
	}
	if want := `cn --config ./swobu.continue.yaml --print Explain this codebase`; continueCmd != want {
		t.Fatalf("continue display command = %q want %q", continueCmd, want)
	}

	openCode, ok := effect.RunClientDisplayCommand("opencode", baseURL, "")
	if !ok {
		t.Fatal("missing opencode display command")
	}
	if !strings.Contains(openCode, "opencode -p Explain this codebase -q") {
		t.Fatalf("opencode display command = %q", openCode)
	}
}
