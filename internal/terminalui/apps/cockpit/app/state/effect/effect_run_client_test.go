package effect

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/compatibility"
)

func assertNoTestHarnessArtifacts(t *testing.T, value string) {
	t.Helper()
	lower := strings.ToLower(strings.TrimSpace(value))
	for _, token := range []string{
		"hermetic-",
		"reply with exactly:",
		"swobu-local",
	} {
		if strings.Contains(lower, token) {
			t.Fatalf("found test-harness artifact token %q in value %q", token, value)
		}
	}
}

func TestRunClientOnceMessage_ValidatesInputs(t *testing.T) {
	t.Parallel()
	if got := runClientOnceMessage(context.Background(), "", "codex", ""); got != "select a workspace before run once" {
		t.Fatalf("message=%q", got)
	}
	if got := runClientOnceMessage(context.Background(), "http://127.0.0.1:7777/c/acme/", "", ""); got != "choose a client before run once" {
		t.Fatalf("message=%q", got)
	}
	if got := runClientOnceMessage(context.Background(), "http://127.0.0.1:7777/c/acme/", "other", ""); got != "run once is not configured for this client yet" {
		t.Fatalf("message=%q", got)
	}
}

func TestRunClientOnceMessage_ExecutableMissing(t *testing.T) {
	origFind := findClientExecutable
	origRun := runForegroundClient
	findClientExecutable = func(string) (string, error) { return "", errors.New("missing") }
	runForegroundClient = func(context.Context, string, []string, map[string]string) (int, error) { return 0, nil }
	t.Cleanup(func() {
		findClientExecutable = origFind
		runForegroundClient = origRun
	})

	got := runClientOnceMessage(context.Background(), "http://127.0.0.1:7777/c/acme/", "aider", "")
	if got != "aider not found in PATH" {
		t.Fatalf("message=%q", got)
	}
}

func TestRunClientOnceMessage_RunFailureIncludesErrorDetail(t *testing.T) {
	origFind := findClientExecutable
	origRun := runForegroundClient
	findClientExecutable = func(binary string) (string, error) { return "/usr/bin/" + binary, nil }
	runForegroundClient = func(context.Context, string, []string, map[string]string) (int, error) {
		return 0, errors.New("permission denied: exec blocked")
	}
	t.Cleanup(func() {
		findClientExecutable = origFind
		runForegroundClient = origRun
	})

	got := runClientOnceMessage(context.Background(), "http://127.0.0.1:7777/c/acme/", "aider", "")
	if got != "failed to start aider: permission denied: exec blocked" {
		t.Fatalf("message=%q", got)
	}
}

func TestRunClientOnceMessage_Success(t *testing.T) {
	origFind := findClientExecutable
	origRun := runForegroundClient
	findClientExecutable = func(binary string) (string, error) { return "/usr/bin/" + binary, nil }
	runForegroundClient = func(context.Context, string, []string, map[string]string) (int, error) { return 0, nil }
	t.Cleanup(func() {
		findClientExecutable = origFind
		runForegroundClient = origRun
	})

	for _, clientID := range []string{"aider", "codex", "claude", "opencode", "continue"} {
		got := runClientOnceMessage(context.Background(), "http://127.0.0.1:7777/c/acme/", clientID, "")
		want := clientID + " exited with code 0"
		if clientID == "continue" {
			want = "cn exited with code 0"
		}
		if got != want {
			t.Fatalf("client %q message=%q", clientID, got)
		}
	}
}

func TestClientRunSpecForID(t *testing.T) {
	spec, ok := clientRunSpecForID("aider", "http://127.0.0.1:7777/c/acme/", "")
	if !ok || spec.binary != "aider" {
		t.Fatalf("spec=%+v ok=%v", spec, ok)
	}
	if got := spec.env["AIDER_OPENAI_API_BASE"]; got != "http://127.0.0.1:7777/c/acme/v1" {
		t.Fatalf("AIDER_OPENAI_API_BASE=%q", got)
	}
	if got := spec.env["OPENAI_API_KEY"]; got != "swobu-placeholder" {
		t.Fatalf("OPENAI_API_KEY=%q", got)
	}
	joinedAiderArgs := strings.Join(spec.args, " ")
	if got := joinedAiderArgs; got != "--model openai/"+compatibility.PrimaryTargetSelector {
		t.Fatalf("aider args=%q", got)
	}
	if strings.Contains(joinedAiderArgs, "hermetic-aider-token") {
		t.Fatalf("aider args=%v", spec.args)
	}
	codex, ok := clientRunSpecForID("codex", "http://127.0.0.1:7777/c/acme/", "")
	if !ok || codex.binary != "codex" {
		t.Fatalf("codex spec=%+v ok=%v", codex, ok)
	}
	if got := strings.Join(codex.args, " "); got != `-c model="`+compatibility.PrimaryTargetSelector+`" -c model_provider="swobu" -c model_providers.swobu.name="Swobu" -c model_providers.swobu.base_url="http://127.0.0.1:7777/c/acme/v1" -c forced_login_method="api"` {
		t.Fatalf("codex args=%q", got)
	}
	claude, ok := clientRunSpecForID("claude", "http://127.0.0.1:7777/c/acme/", "")
	if !ok || claude.binary != "claude" {
		t.Fatalf("claude spec=%+v ok=%v", claude, ok)
	}
	if got := claude.env["ANTHROPIC_BASE_URL"]; got != "http://127.0.0.1:7777/c/acme/" {
		t.Fatalf("claude env ANTHROPIC_BASE_URL=%q", got)
	}
	if got := claude.env["ANTHROPIC_MODEL"]; got != compatibility.PrimaryTargetSelector {
		t.Fatalf("claude env ANTHROPIC_MODEL=%q", got)
	}
	opencode, ok := clientRunSpecForID("opencode", "http://127.0.0.1:7777/c/acme/", "")
	if !ok || opencode.binary != "opencode" {
		t.Fatalf("opencode spec=%+v ok=%v", opencode, ok)
	}
	if got := opencode.env["OPENAI_API_KEY"]; got != "swobu-placeholder" {
		t.Fatalf("opencode env OPENAI_API_KEY=%q", got)
	}
	if got := opencode.env["OPENCODE_CONFIG_CONTENT"]; got != "" {
		t.Fatalf("opencode env OPENCODE_CONFIG_CONTENT must be empty, got=%q", got)
	}
	if opencode.prepare == nil {
		t.Fatal("opencode prepare missing")
	}
	if got := strings.Join(opencode.args, " "); got != `run --model swobu/`+compatibility.PrimaryTargetSelector+` Explain this codebase` {
		t.Fatalf("opencode args=%q", got)
	}
	continueSpec, ok := clientRunSpecForID("continue", "http://127.0.0.1:7777/c/acme/", "")
	if !ok || continueSpec.binary != "cn" {
		t.Fatalf("continue spec=%+v ok=%v", continueSpec, ok)
	}
	if got := strings.Join(continueSpec.args, " "); got != `--config ./swobu.continue.yaml --print Explain this codebase` {
		t.Fatalf("continue args=%q", got)
	}
}

func TestRunClientDisplayCommand(t *testing.T) {
	cmd, ok := RunClientDisplayCommand("aider", "http://127.0.0.1:7777/c/acme/", "")
	if !ok {
		t.Fatal("aider command missing")
	}
	if want := "AIDER_OPENAI_API_BASE=http://127.0.0.1:7777/c/acme/v1 aider --model openai/" + compatibility.PrimaryTargetSelector; cmd != want {
		t.Fatalf("aider command=%q want=%q", cmd, want)
	}
	assertNoTestHarnessArtifacts(t, cmd)
	codex, ok := RunClientDisplayCommand("codex", "http://127.0.0.1:7777/c/acme/", "")
	if !ok {
		t.Fatal("codex command missing")
	}
	if want := `codex -c model="` + compatibility.PrimaryTargetSelector + `" -c model_provider="swobu" -c model_providers.swobu.name="Swobu" -c model_providers.swobu.base_url="http://127.0.0.1:7777/c/acme/v1" -c forced_login_method="api"`; codex != want {
		t.Fatalf("codex command=%q want=%q", codex, want)
	}
	assertNoTestHarnessArtifacts(t, codex)
	claude, ok := RunClientDisplayCommand("claude", "http://127.0.0.1:7777/c/acme/", "")
	if !ok {
		t.Fatal("claude command missing")
	}
	if want := "ANTHROPIC_BASE_URL=http://127.0.0.1:7777/c/acme/ ANTHROPIC_MODEL=" + compatibility.PrimaryTargetSelector + " claude --model " + compatibility.PrimaryTargetSelector; claude != want {
		t.Fatalf("claude command=%q want=%q", claude, want)
	}
	assertNoTestHarnessArtifacts(t, claude)
	opencode, ok := RunClientDisplayCommand("opencode", "http://127.0.0.1:7777/c/acme/", "")
	if !ok {
		t.Fatal("opencode command missing")
	}
	if want := `opencode -p Explain this codebase -q`; opencode != want {
		t.Fatalf("opencode command=%q want=%q", opencode, want)
	}
	assertNoTestHarnessArtifacts(t, opencode)
	continueCmd, ok := RunClientDisplayCommand("continue", "http://127.0.0.1:7777/c/acme/", "")
	if !ok {
		t.Fatal("continue command missing")
	}
	if want := `cn --config ./swobu.continue.yaml --print Explain this codebase`; continueCmd != want {
		t.Fatalf("continue command=%q want=%q", continueCmd, want)
	}
	assertNoTestHarnessArtifacts(t, continueCmd)
}

func TestRunClientOnceMessage_ContinueWritesConfigWhenMissing(t *testing.T) {
	origFind := findClientExecutable
	origRun := runForegroundClient
	findClientExecutable = func(binary string) (string, error) { return "/usr/bin/" + binary, nil }
	runForegroundClient = func(context.Context, string, []string, map[string]string) (int, error) { return 0, nil }
	t.Cleanup(func() {
		findClientExecutable = origFind
		runForegroundClient = origRun
	})

	tempDir := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Chdir temp: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	got := runClientOnceMessage(context.Background(), "http://127.0.0.1:7777/c/acme/", "continue", "")
	if got != "cn exited with code 0" {
		t.Fatalf("message=%q", got)
	}
	body, err := os.ReadFile("swobu.continue.yaml")
	if err != nil {
		t.Fatalf("read continue config: %v", err)
	}
	if !strings.Contains(string(body), "apiBase: http://127.0.0.1:7777/c/acme/v1") {
		t.Fatalf("continue config body=%q", string(body))
	}
}
