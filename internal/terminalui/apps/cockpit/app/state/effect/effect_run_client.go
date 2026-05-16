package effect

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
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
	if strings.TrimSpace(baseURL) == "" || baseURL == "none" { // trimlowerlint:allow boundary canonicalization
		return "select a workspace before run once"
	}
	clientID = strings.TrimSpace(clientID) // trimlowerlint:allow boundary canonicalization
	if clientID == "" {
		return "choose a client before run once"
	}
	spec, ok := clientRunSpecForID(clientID, baseURL, modelID)
	if !ok {
		return "run once is not configured for this client yet"
	}
	if spec.prepare != nil {
		if err := spec.prepare(); err != nil {
			return "failed to start " + spec.binary + ": " + strings.TrimSpace(err.Error()) // trimlowerlint:allow boundary canonicalization
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
			return "run once is already active"
		}
		if errors.Is(err, ErrForegroundClientUnavailable) {
			return "run once is unavailable until cockpit is active"
		}
		return "failed to start " + spec.binary + ": " + strings.TrimSpace(err.Error()) // trimlowerlint:allow boundary canonicalization
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
		clientID: strings.TrimSpace(command.ClientID), // trimlowerlint:allow boundary canonicalization
		binary:   strings.TrimSpace(command.Binary),   // trimlowerlint:allow boundary canonicalization
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
	path := strings.TrimSpace(prepare.Path) // trimlowerlint:allow boundary canonicalization
	if path == "" {
		return fmt.Errorf("empty run preparation file path")
	}
	if prepare.WriteIfMissing {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
	}
	mode := prepare.Mode
	if mode == 0 {
		mode = 0o600
	}
	return os.WriteFile(path, []byte(prepare.Content), mode)
}

// RunClientDisplayCommand returns the shell-style command line that corresponds
// to the run-once invocation for the selected client and model context.
func RunClientDisplayCommand(clientID, baseURL, modelID string) (string, bool) {
	spec, ok := clientRunSpecForID(clientID, baseURL, modelID)
	if !ok {
		return "", false
	}
	parts := make([]string, 0, len(spec.env)+1+len(spec.args))
	if len(spec.env) > 0 {
		keys := make([]string, 0, len(spec.env))
		for key := range spec.env {
			if strings.EqualFold(strings.TrimSpace(key), "OPENAI_API_KEY") { // trimlowerlint:allow boundary canonicalization
				continue
			}
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			parts = append(parts, key+"="+spec.env[key])
		}
	}
	parts = append(parts, spec.binary)
	parts = append(parts, spec.args...)
	return strings.Join(parts, " "), true
}
