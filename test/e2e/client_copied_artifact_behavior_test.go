package e2e_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/metrofun/swobu/internal/app/operator/clientprofile"
	"github.com/metrofun/swobu/internal/domain/endpointintent"
	"github.com/metrofun/swobu/internal/domain/protocolsurface"
	"github.com/metrofun/swobu/test/e2e/harness"
	"github.com/metrofun/swobu/test/framework/ptykit"
)

// Proves copied client artifacts are behaviorally correct by running real
// client binaries against a live Swobu endpoint and asserting captured I/O.
func TestClientCopiedArtifacts_ExecuteAgainstSwobu_OpenAICompat(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("client artifact behavior e2e currently targets unix-style shells")
	}
	requireBinaries(t, "aider", "opencode", "cn", "codex", "claude")

	openAIUpstream := newHermeticOpenAICompatibleReplayUpstream()
	defer openAIUpstream.Close()
	anthropicUpstream := newHermeticAnthropicReplayUpstream()
	defer anthropicUpstream.Close()

	openAIProvider := mustProviderConfigWithModelID(
		t,
		harness.NewProviderConfig(
			t,
			"custom-main",
			"custom",
			openAIUpstream.URL+"/v1",
			"",
			protocolsurface.ChatCompletions,
		),
		"gpt-4.1-mini",
	)
	anthropicProvider := mustProviderConfigWithModelID(
		t,
		harness.NewProviderConfig(
			t,
			"anthropic-main",
			"anthropic",
			anthropicUpstream.URL+"/v1",
			"",
			protocolsurface.Messages,
		),
		"claude-3-7-sonnet",
	)
	daemon := harness.StartDaemonProcess(t, harness.DaemonProcessConfig{
		Endpoints: []endpointintent.Endpoint{
			harness.NewEndpoint(t, "jobs", "custom-main", openAIProvider),
			harness.NewEndpoint(t, "anthropic", "anthropic-main", anthropicProvider),
		},
	})

	recorder := newRequestForwardRecorderEphemeral(t, daemon.BaseURL)
	defer recorder.Close()

	openAIBaseURL := strings.TrimRight(recorder.BaseURL(), "/") + "/c/jobs/"
	anthropicBaseURL := strings.TrimRight(recorder.BaseURL(), "/") + "/c/anthropic/"
	profiles := clientprofile.Catalog()

	t.Run("codex-file-config-copy-is-runnable", func(t *testing.T) {
		profile := mustProfileByID(t, profiles, "codex")
		content := mustActionByID(t, profile.Actions(openAIBaseURL), "file-config").Content

		home := t.TempDir()
		configDir := filepath.Join(home, ".codex")
		if err := os.MkdirAll(configDir, 0o755); err != nil {
			t.Fatalf("mkdir codex config dir: %v", err)
		}
		configPath := filepath.Join(configDir, "config.toml")
		if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
			t.Fatalf("write codex config: %v", err)
		}

		if !strings.Contains(content, `model_provider = "swobu"`) {
			t.Fatalf("copied codex config missing model provider: %q", content)
		}
		if !strings.Contains(content, `base_url = "`+openAIBaseURL+`v1"`) {
			t.Fatalf("copied codex config missing base_url: %q", content)
		}
		before := recorder.CompatibilityRequestCount()
		out, err := runClientCommand(t, home, map[string]string{
			"OPENAI_API_KEY":  "swobu-hermetic-key",
			"HOME":            home,
			"XDG_CONFIG_HOME": home,
			"XDG_STATE_HOME":  home,
			"XDG_CACHE_HOME":  home,
			"CODEX_HOME":      configDir,
		}, "codex", "exec", "--skip-git-repo-check", "--color", "never", "Reply with exactly: hermetic-codex-copy")
		if err != nil {
			t.Fatalf("codex command failed: %v\noutput:\n%s", err, out)
		}
		assertCompatibilityRequestIncrementWithPath(t, recorder, before, "codex file-config", "/c/jobs/", out)
	})

	t.Run("continue-file-config-copy-is-runnable", func(t *testing.T) {
		profile := mustProfileByID(t, profiles, "continue")
		content := mustActionByID(t, profile.Actions(openAIBaseURL), "file-config").Content

		work := t.TempDir()
		configPath := filepath.Join(work, "swobu.continue.yaml")
		if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
			t.Fatalf("write continue config: %v", err)
		}

		before := recorder.CompatibilityRequestCount()
		out, err := runClientCommand(t, work, map[string]string{
			"OPENAI_API_KEY": "swobu-hermetic-key",
		}, "cn", "--config", "./swobu.continue.yaml", "--print", "Reply with exactly: hermetic-continue-copy")
		if err != nil {
			t.Fatalf("continue command failed: %v\noutput:\n%s", err, out)
		}
		assertCompatibilityRequestIncrementWithPath(t, recorder, before, "continue file-config", "/c/jobs/", out)
	})

	t.Run("opencode-file-config-copy-is-runnable", func(t *testing.T) {
		profile := mustProfileByID(t, profiles, "opencode")
		content := mustActionByID(t, profile.Actions(openAIBaseURL), "file-config").Content

		work := t.TempDir()
		configPath := filepath.Join(work, "opencode.json")
		if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
			t.Fatalf("write opencode config: %v", err)
		}

		before := recorder.CompatibilityRequestCount()
		out, err := runClientCommand(t, work, map[string]string{
			"OPENAI_API_KEY": "swobu-hermetic-key",
		}, "opencode", "run", "--model", "swobu/primary", "Reply with exactly: hermetic-opencode-copy")
		if err != nil {
			t.Fatalf("opencode command failed: %v\noutput:\n%s", err, out)
		}
		assertCompatibilityRequestIncrementWithPath(t, recorder, before, "opencode file-config", "/c/jobs/", out)
	})

	t.Run("aider-env-copy-is-runnable", func(t *testing.T) {
		profile := mustProfileByID(t, profiles, "aider")
		envCopy := mustActionByID(t, profile.Actions(openAIBaseURL), "env-copy").Content
		env := parseCopiedEnv(envCopy)
		env["OPENAI_API_KEY"] = "swobu-hermetic-key"
		env["AIDER_CHECK_UPDATE"] = "false"
		env["AIDER_SHOW_RELEASE_NOTES"] = "false"
		env["AIDER_SHOW_MODEL_WARNINGS"] = "false"
		env["AIDER_CHECK_MODEL_ACCEPTS_SETTINGS"] = "false"
		env["AIDER_ANALYTICS"] = "false"
		env["BROWSER"] = "/bin/false"

		work := t.TempDir()
		before := recorder.CompatibilityRequestCount()
		out, err := runClientCommand(t, work, env,
			"aider",
			"--model", "openai/primary",
			"--message", "Reply with exactly: hermetic-aider-env-copy",
			"--yes-always",
			"--no-git",
			"--no-check-update",
			"--no-show-release-notes",
			"--no-show-model-warnings",
			"--no-check-model-accepts-settings",
			"--analytics-disable",
			"--no-browser",
			"--no-gui",
			"--exit",
		)
		if err != nil {
			t.Fatalf("aider env-copy command failed: %v\noutput:\n%s", err, out)
		}
		assertCompatibilityRequestIncrementWithPath(t, recorder, before, "aider env-copy", "/c/jobs/", out)
	})

	t.Run("claude-env-copy-is-runnable", func(t *testing.T) {
		profile := mustProfileByID(t, profiles, "claude")
		envCopy := mustActionByID(t, profile.Actions(anthropicBaseURL), "env-copy").Content
		env := parseCopiedEnv(envCopy)
		env["ANTHROPIC_API_KEY"] = "swobu-hermetic-key"
		env["CLAUDE_CODE_SIMPLE"] = "1"

		work := t.TempDir()
		before := recorder.CompatibilityRequestCount()
		out, err := runClientHeadfulPTYUntilRequest(t, work, env, recorder, before, "/c/anthropic/", "claude", "--bare", "--dangerously-skip-permissions", "--model", "primary", "Reply with exactly: hermetic-claude-env-copy")
		if err != nil {
			t.Fatalf("claude env-copy headful command failed: %v\noutput:\n%s", err, out)
		}
		assertCompatibilityRequestIncrementWithPath(t, recorder, before, "claude env-copy", "/c/anthropic/", out)
	})
}

func requireBinaries(t *testing.T, names ...string) {
	t.Helper()
	for _, name := range names {
		if _, err := exec.LookPath(name); err != nil {
			t.Fatalf("%s binary is required for this test: %v", name, err)
		}
	}
}

func mustProfileByID(t *testing.T, profiles []clientprofile.Profile, id string) clientprofile.Profile {
	t.Helper()
	profile := clientprofile.FindByID(profiles, id)
	if profile == nil {
		t.Fatalf("missing client profile %q", id)
	}
	return profile
}

func mustActionByID(t *testing.T, actions []clientprofile.Action, id string) clientprofile.Action {
	t.Helper()
	for _, action := range actions {
		if strings.TrimSpace(action.ID) == strings.TrimSpace(id) {
			return action
		}
	}
	t.Fatalf("missing action id %q", id)
	return clientprofile.Action{}
}

func parseCopiedEnv(content string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.Index(line, "=")
		if eq <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		value := strings.TrimSpace(line[eq+1:])
		if key == "" {
			continue
		}
		out[key] = value
	}
	return out
}

func runClientCommand(t *testing.T, workdir string, extraEnv map[string]string, binary string, args ...string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = workdir
	cmd.Env = os.Environ()
	for key, value := range extraEnv {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return out.String(), ctx.Err()
	}
	return out.String(), err
}

func runClientHeadfulPTYUntilRequest(t *testing.T, workdir string, extraEnv map[string]string, recorder *requestForwardRecorder, beforeCount int, expectedPathPrefix string, binary string, args ...string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmd := exec.Command(binary, args...)
	cmd.Dir = workdir
	cmd.Env = os.Environ()
	for key, value := range extraEnv {
		cmd.Env = append(cmd.Env, key+"="+value)
	}

	run, err := ptykit.StartCommandWithSize(cmd, 120, 36)
	if err != nil {
		return "", err
	}
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		_ = run.Shutdown(shutdownCtx)
	}()

	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()
	trustAcks := 0
	apiKeyAcks := 0

	for {
		after := recorder.CompatibilityRequestCount()
		if after > beforeCount {
			last, ok := recorder.LastCompatibilityRequest()
			if ok && strings.HasPrefix(strings.TrimSpace(last.Path), strings.TrimSpace(expectedPathPrefix)) {
				_ = run.SendKey("CtrlC")
				return run.Output(), nil
			}
		}
		visible := strings.ToLower(run.VisibleOutput())
		if trustAcks < 5 && strings.Contains(visible, "yes, i trust this folder") {
			_ = run.SendKey("Enter")
			trustAcks++
		}
		if apiKeyAcks < 5 && strings.Contains(visible, "do you want to use this api key") {
			_ = run.SendRaw("1")
			_ = run.SendKey("Enter")
			apiKeyAcks++
		}
		select {
		case <-ctx.Done():
			return run.Output(), ctx.Err()
		case <-ticker.C:
		}
	}
}

func assertCompatibilityRequestIncrementWithPath(t *testing.T, recorder *requestForwardRecorder, before int, label, pathPrefix, output string) {
	t.Helper()
	after := recorder.CompatibilityRequestCount()
	last, ok := recorder.LastCompatibilityRequest()
	if after > before && strings.TrimSpace(pathPrefix) == "" {
		return
	}
	if !ok {
		t.Fatalf("%s: no compatibility request captured; output=%q", label, output)
	}
	if after > before && strings.TrimSpace(pathPrefix) != "" {
		if strings.HasPrefix(strings.TrimSpace(last.Path), strings.TrimSpace(pathPrefix)) {
			return
		}
		t.Fatalf("%s: request increased but last path %q does not match expected prefix %q; output=%q", label, last.Path, pathPrefix, output)
	}
	t.Fatalf("%s: request count did not increase (before=%d after=%d last_path=%q); output=%q", label, before, after, last.Path, output)
}

func newHermeticAnthropicReplayUpstream() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/messages":
			raw, _ := io.ReadAll(r.Body)
			_ = r.Body.Close()
			if bytes.Contains(bytes.ToLower(raw), []byte(`"stream":true`)) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte(strings.Join([]string{
					`event: message_start`,
					`data: {"type":"message_start","message":{"id":"msg_1","model":"primary"}}`,
					``,
					`event: content_block_start`,
					`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
					``,
					`event: content_block_delta`,
					`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}`,
					``,
					`event: message_delta`,
					`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"}}`,
					``,
					`event: message_stop`,
					`data: {"type":"message_stop"}`,
					``,
				}, "\n")))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"msg_1","model":"primary","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn"}`))
			return
		case "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"primary","object":"model"}]}`))
			return
		default:
			http.NotFound(w, r)
		}
	}))
}
