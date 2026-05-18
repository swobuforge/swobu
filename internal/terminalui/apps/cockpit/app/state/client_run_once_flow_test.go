package state

import (
	"context"
	"os"
	"os/exec"
	"testing"

	stateeffect "github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state/effect"
)

func TestReduce_ClientLaunchRequested_EndToEndLaunchFlow(t *testing.T) {
	tests := []struct {
		name         string
		preset       string
		binaryName   string
		expectedNote string
	}{
		{name: "codex", preset: "codex", binaryName: "codex", expectedNote: "codex exited with code 0"},
		{name: "claude", preset: "claude", binaryName: "claude", expectedNote: "claude exited with code 0"},
		{name: "aider", preset: "aider", binaryName: "aider", expectedNote: "aider exited with code 0"},
		{name: "continue", preset: "continue", binaryName: "cn", expectedNote: "cn exited with code 0"},
		{name: "opencode", preset: "opencode", binaryName: "opencode", expectedNote: "opencode exited with code 0"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := exec.LookPath(tc.binaryName); err != nil {
				t.Fatalf("%s binary is required for this test: %v", tc.binaryName, err)
			}
			tempDir := t.TempDir()
			cwd, err := os.Getwd()
			if err != nil {
				t.Fatalf("Getwd: %v", err)
			}
			if err := os.Chdir(tempDir); err != nil {
				t.Fatalf("Chdir temp: %v", err)
			}
			t.Cleanup(func() { _ = os.Chdir(cwd) })

			restore := stateeffect.SetForegroundClientRunner(func(context.Context, string, []string, map[string]string) (int, error) {
				return 0, nil
			})
			t.Cleanup(restore)

			model := Model{
				CurrentEndpoint: "acme",
			}
			effects := Reduce(&model, ClientLaunchRequestedAction{
				BaseURL: "http://127.0.0.1:7926/c/acme/",
				Preset:  tc.preset,
			})
			if got := model.HeaderStatus; got != "running…" {
				t.Fatalf("header status after launch request = %q, want %q", got, "running…")
			}
			if got := model.InteractionMode; got != InteractionModeBusyLaunch {
				t.Fatalf("interaction mode after launch request = %q, want %q", got, InteractionModeBusyLaunch)
			}
			if len(effects) != 1 {
				t.Fatalf("effects len = %d, want 1", len(effects))
			}
			launch, ok := effects[0].(LaunchClientEffect)
			if !ok {
				t.Fatalf("effect type = %T, want LaunchClientEffect", effects[0])
			}

			actions := launch.Execute(context.Background())
			if len(actions) != 1 {
				t.Fatalf("actions len = %d, want 1", len(actions))
			}
			Reduce(&model, actions[0])
			if got := model.HeaderStatus; got != "ready" {
				t.Fatalf("header status after launch execute = %q, want %q", got, "ready")
			}
			if got := model.InteractionMode; got != InteractionModeNAV {
				t.Fatalf("interaction mode after launch execute = %q, want %q", got, InteractionModeNAV)
			}
			if got := model.ClientLaunchNote; got != tc.expectedNote {
				t.Fatalf("launch note = %q, want %q", got, tc.expectedNote)
			}
		})
	}
}
