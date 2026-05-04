package clientprofile

import (
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/compatibility"
)

func TestResolveRunCommand_RunnableProfiles(t *testing.T) {
	t.Parallel()

	baseURL := "http://127.0.0.1:7926/c/acme/"
	tests := []struct {
		clientID    string
		binary      string
		contains    []string
		envChecks   map[string]string
		preparePath string
	}{
		{
			clientID: "aider",
			binary:   "aider",
			contains: []string{"--model", "openai/" + compatibility.PrimaryTargetSelector},
			envChecks: map[string]string{
				"AIDER_OPENAI_API_BASE": "http://127.0.0.1:7926/c/acme/v1",
				"OPENAI_API_KEY":        "swobu-placeholder",
			},
		},
		{
			clientID: "codex",
			binary:   "codex",
			contains: []string{
				`model="` + compatibility.PrimaryTargetSelector + `"`,
				`model_provider="swobu"`,
				`model_providers.swobu.base_url="http://127.0.0.1:7926/c/acme/v1"`,
				`forced_login_method="api"`,
			},
		},
		{
			clientID: "claude",
			binary:   "claude",
			contains: []string{"--model", compatibility.PrimaryTargetSelector},
			envChecks: map[string]string{
				"ANTHROPIC_BASE_URL": "http://127.0.0.1:7926/c/acme/",
				"ANTHROPIC_MODEL":    compatibility.PrimaryTargetSelector,
			},
		},
		{
			clientID:    "continue",
			binary:      "cn",
			contains:    []string{"--config", "./swobu.continue.yaml", "--print", "Explain this codebase"},
			preparePath: "./swobu.continue.yaml",
		},
		{
			clientID: "opencode",
			binary:   "opencode",
			contains: []string{"run", "--model", "swobu/" + compatibility.PrimaryTargetSelector, "Explain this codebase"},
			envChecks: map[string]string{
				"OPENAI_API_KEY": "swobu-placeholder",
			},
			preparePath: "./opencode.json",
		},
	}

	for _, tc := range tests {
		t.Run(tc.clientID, func(t *testing.T) {
			t.Parallel()
			command, ok := ResolveRunCommand(tc.clientID, baseURL, "")
			if !ok {
				t.Fatalf("ResolveRunCommand(%q) returned not ok", tc.clientID)
			}
			if command.Binary != tc.binary {
				t.Fatalf("binary=%q want=%q", command.Binary, tc.binary)
			}
			joined := strings.Join(command.Args, " ")
			for _, fragment := range tc.contains {
				if !strings.Contains(joined, fragment) {
					t.Fatalf("args=%q missing fragment=%q", joined, fragment)
				}
			}
			for key, contains := range tc.envChecks {
				got := command.Env[key]
				if !strings.Contains(got, contains) {
					t.Fatalf("env[%q]=%q missing %q", key, got, contains)
				}
			}
			if tc.preparePath == "" {
				if command.Prepare != nil {
					t.Fatalf("prepare unexpectedly set: %+v", *command.Prepare)
				}
				return
			}
			if command.Prepare == nil {
				t.Fatalf("prepare missing")
			}
			if command.Prepare.Path != tc.preparePath {
				t.Fatalf("prepare path=%q want=%q", command.Prepare.Path, tc.preparePath)
			}
			if !strings.Contains(command.Prepare.Content, "http://127.0.0.1:7926/c/acme/v1") {
				t.Fatalf("prepare content=%q", command.Prepare.Content)
			}
		})
	}
}

func TestResolveRunCommand_NonRunnableProfiles(t *testing.T) {
	t.Parallel()

	if _, ok := ResolveRunCommand("other", "http://127.0.0.1:7926/c/acme/", ""); ok {
		t.Fatal("other must not resolve run command")
	}
	if _, ok := ResolveRunCommand("", "http://127.0.0.1:7926/c/acme/", ""); ok {
		t.Fatal("empty client must not resolve run command")
	}
}
