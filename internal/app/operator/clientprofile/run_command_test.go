package clientprofile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/exchange"
)

func TestResolveRunCommand_RunnableProfiles(t *testing.T) {
	t.Parallel()

	baseURL := "http://127.0.0.1:7926/c/acme/"
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	opencodeConfigPath := filepath.Join(cwd, "opencode.json")
	piConfigPath := filepath.Join(cwd, ".pi", "agent")
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
			contains: []string{"--no-show-model-warnings", "--no-browser", "--model", "openai/" + exchange.PublicModelIDSwobu},
			envChecks: map[string]string{
				"AIDER_OPENAI_API_BASE": "http://127.0.0.1:7926/c/acme/v1",
				"OPENAI_API_KEY":        "swobu-placeholder",
			},
		},
		{
			clientID: "codex",
			binary:   "codex",
			contains: []string{
				`--dangerously-bypass-approvals-and-sandbox`,
				`model="gpt-5.5"`,
				`model_provider="swobu"`,
				`model_providers.swobu.base_url="http://127.0.0.1:7926/c/acme/v1"`,
			},
			envChecks: map[string]string{
				"OPENAI_API_KEY": "swobu-placeholder",
			},
		},
		{
			clientID: "claude",
			binary:   "claude",
			contains: []string{"--bare", "--add-dir", ".", "--tools", "Read", "--allowedTools", "Read", "--model", exchange.PublicModelIDSwobu},
			envChecks: map[string]string{
				"ANTHROPIC_API_KEY":  "swobu-placeholder",
				"ANTHROPIC_BASE_URL": "http://127.0.0.1:7926/c/acme/",
				"ANTHROPIC_MODEL":    exchange.PublicModelIDSwobu,
			},
		},
		{
			clientID:    "continue",
			binary:      "cn",
			contains:    []string{"--config", "./swobu.continue.yaml"},
			preparePath: "./swobu.continue.yaml",
		},
		{
			clientID: "opencode",
			binary:   "opencode",
			contains: []string{},
			envChecks: map[string]string{
				"OPENAI_API_KEY":          "swobu-placeholder",
				"OPENCODE_CONFIG":         opencodeConfigPath,
				"OPENCODE_CONFIG_CONTENT": `"apiKey":"{env:OPENAI_API_KEY}"`,
			},
			preparePath: "./opencode.json",
		},
		{
			clientID: "pi",
			binary:   "pi",
			contains: []string{"--no-context-files", "--no-skills", "--provider", "swobu", "--model", "gpt-4.1-mini"},
			envChecks: map[string]string{
				"OPENAI_API_KEY":      "swobu-placeholder",
				"PI_CODING_AGENT_DIR": piConfigPath,
				"PI_OFFLINE":          "1",
			},
			preparePath: "./.pi/agent/models.json",
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
			if tc.clientID == "opencode" && strings.TrimSpace(joined) != "" {
				t.Fatalf("opencode args=%q want empty for interactive launch", joined)
			}
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
			if tc.clientID == "pi" {
				for _, fragment := range []string{
					`"api":"openai-responses"`,
					`"apiKey":"swobu-placeholder"`,
					`"authHeader":true`,
					`"supportsDeveloperRole":false`,
					`"id":"gpt-4.1-mini"`,
				} {
					if !strings.Contains(command.Prepare.Content, fragment) {
						t.Fatalf("pi prepare content=%q missing fragment=%q", command.Prepare.Content, fragment)
					}
				}
			}
		})
	}
}

func TestResolveRunCommand_RejectsBatchProbeArgs(t *testing.T) {
	t.Parallel()

	baseURL := "http://127.0.0.1:7926/c/acme/"
	for _, clientID := range []string{"codex", "aider", "claude", "continue", "opencode", "pi"} {
		command, ok := ResolveRunCommand(clientID, baseURL, "")
		if !ok {
			t.Fatalf("ResolveRunCommand(%q) returned not ok", clientID)
		}
		joined := strings.ToLower(strings.Join(command.Args, " "))
		for _, banned := range []string{
			"reply with exactly:",
			"--message",
			"--exit",
			" -p ",
		} {
			if strings.Contains(" "+joined+" ", banned) {
				t.Fatalf("client %q run args must be interactive-only, found banned token %q in %q", clientID, banned, joined)
			}
		}
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
