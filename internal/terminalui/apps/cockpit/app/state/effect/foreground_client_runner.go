package effect

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/swobuforge/swobu/internal/app/operator/clientprofile"
)

type clientRunSpec struct {
	clientID string
	binary   string
	args     []string
	env      map[string]string
	prepare  func() error
}

var (
	findClientExecutable = exec.LookPath
	runForegroundClient  = runForegroundClientUnavailable
	foregroundRunnerMu   sync.RWMutex
)

// ErrForegroundClientUnavailable reports that no cockpit runtime is available
// to perform terminal foreground handoff.
var ErrForegroundClientUnavailable = errors.New("foreground handoff unavailable")

// ErrForegroundClientActive reports that a foreground client run is already in
// progress and a second run cannot start.
var ErrForegroundClientActive = errors.New("foreground handoff already active")

func runForegroundClientUnavailable(context.Context, string, []string, map[string]string) (int, error) {
	return 0, ErrForegroundClientUnavailable
}

// SetForegroundClientRunner installs the runtime-owned foreground launcher
// used by run-once effects and returns a restore function.
func SetForegroundClientRunner(run func(context.Context, string, []string, map[string]string) (int, error)) func() {
	if run == nil {
		run = runForegroundClientUnavailable
	}
	foregroundRunnerMu.Lock()
	previous := runForegroundClient
	runForegroundClient = run
	foregroundRunnerMu.Unlock()
	return func() {
		foregroundRunnerMu.Lock()
		runForegroundClient = previous
		foregroundRunnerMu.Unlock()
	}
}

func runClientOnceMessage(ctx context.Context, baseURL, clientID, modelID string) string {
	if strings.TrimSpace(baseURL) == "" || baseURL == "none" { // swobu:io-string source=boundary
		return "select a workspace before run"
	}
	clientID = strings.TrimSpace(clientID) // swobu:io-string source=boundary
	if clientID == "" {
		return "choose a client before run"
	}
	spec, ok := clientRunSpecForID(clientID, baseURL, modelID)
	if !ok {
		return "run is not configured for this client yet"
	}
	if spec.prepare != nil {
		if err := spec.prepare(); err != nil {
			return "failed to start " + spec.binary + ": " + strings.TrimSpace(err.Error()) // swobu:io-string source=boundary
		}
	}
	executable, err := findClientExecutable(spec.binary)
	if err != nil {
		return spec.binary + " not found in PATH"
	}
	run := currentForegroundClientRunner()
	exitCode, err := run(ctx, executable, spec.args, spec.env)
	if err != nil {
		if errors.Is(err, ErrForegroundClientActive) {
			return "run is already active"
		}
		if errors.Is(err, ErrForegroundClientUnavailable) {
			return "run is unavailable until cockpit is active"
		}
		return "failed to start " + spec.binary + ": " + strings.TrimSpace(err.Error()) // swobu:io-string source=boundary
	}
	if exitCode != 0 {
		return fmt.Sprintf("%s exited with code %d", spec.binary, exitCode)
	}
	return fmt.Sprintf("%s exited with code 0", spec.binary)
}

func currentForegroundClientRunner() func(context.Context, string, []string, map[string]string) (int, error) {
	foregroundRunnerMu.RLock()
	run := runForegroundClient
	foregroundRunnerMu.RUnlock()
	return run
}

func clientRunSpecForID(clientID, baseURL, modelID string) (clientRunSpec, bool) {
	command, ok := clientprofile.ResolveRunCommand(clientID, baseURL, modelID)
	if !ok {
		return clientRunSpec{}, false
	}
	spec := clientRunSpec{
		clientID: strings.TrimSpace(command.ClientID), // swobu:io-string source=boundary
		binary:   strings.TrimSpace(command.Binary),   // swobu:io-string source=boundary
		args:     append([]string(nil), command.Args...),
		env:      cloneStringMap(command.Env),
	}
	if command.Prepare != nil {
		prepare := *command.Prepare
		spec.prepare = func() error { return ensurePreparedRunFile(prepare) }
	}
	return spec, true
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func ensurePreparedRunFile(prepare clientprofile.RunPrepareFileSpec) error {
	path := strings.TrimSpace(prepare.Path) // swobu:io-string source=boundary
	if path == "" {
		return fmt.Errorf("empty run preparation file path")
	}
	mode := prepare.Mode
	if mode == 0 {
		mode = 0o600
	}
	// Nested config directories are valid for client-specific bootstrap files.
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create run preparation dir %s: %w", filepath.Dir(path), err)
	}
	return os.WriteFile(path, []byte(prepare.Content), mode)
}
