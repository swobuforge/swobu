package clientprofile

import (
	"strings"
	"testing"
)

func TestCatalog_ContainsRunnableProfilesOnly(t *testing.T) {
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
		"pi",
	} {
		if profile := FindByID(profiles, id); profile == nil {
			t.Fatalf("missing profile %q", id)
		}
	}
	if profile := FindByID(profiles, "other"); profile != nil {
		t.Fatalf("unexpected unsupported profile %q", profile.Identity().ID)
	}
}

func TestProfileActions_RunOnlyMatrix(t *testing.T) {
	t.Parallel()

	baseURL := "http://127.0.0.1:7926/c/acme/"
	tests := []struct {
		profileID string
		contains  []string
	}{
		{
			profileID: "codex",
			contains:  []string{"codex --dangerously-bypass-approvals-and-sandbox", `model="gpt-5.5"`, `model_provider="swobu"`},
		},
		{
			profileID: "claude",
			contains:  []string{"claude --bare --add-dir . --tools Read --allowedTools Read --model", "ANTHROPIC_API_KEY=swobu-placeholder", "ANTHROPIC_BASE_URL=http://127.0.0.1:7926/c/acme/"},
		},
		{
			profileID: "aider",
			contains:  []string{"aider --no-show-model-warnings --no-browser", "--model", "AIDER_OPENAI_API_BASE=http://127.0.0.1:7926/c/acme/v1"},
		},
		{
			profileID: "continue",
			contains:  []string{"cn --config ./swobu.continue.yaml"},
		},
		{
			profileID: "opencode",
			contains:  []string{"opencode", `"apiKey":"{env:OPENAI_API_KEY}"`, `OPENCODE_CONFIG_CONTENT=`},
		},
		{
			profileID: "pi",
			contains:  []string{"PI_CODING_AGENT_DIR=", "PI_OFFLINE=1", "pi --no-context-files --no-skills --provider swobu --model gpt-4.1-mini"},
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
			if len(actions) != 1 {
				t.Fatalf("profile %q action count=%d want 1", tc.profileID, len(actions))
			}
			run := actions[0]
			if got := run.RowLabel(); got != "run" {
				t.Fatalf("profile %q row=%q want run", tc.profileID, got)
			}
			if got := run.ActionVerb(); got != "run" {
				t.Fatalf("profile %q verb=%q want run", tc.profileID, got)
			}
			if got := run.ActionSummary(); got != "command" {
				t.Fatalf("profile %q summary=%q want command", tc.profileID, got)
			}
			for _, fragment := range tc.contains {
				if !strings.Contains(run.Content, fragment) {
					t.Fatalf("run content=%q missing fragment=%q", run.Content, fragment)
				}
			}
		})
	}
}
