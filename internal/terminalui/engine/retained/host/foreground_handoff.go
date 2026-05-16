package host

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
)

// ErrForegroundHandoffUnavailable reports that no active host runner can
// perform terminal suspend/resume for a foreground client handoff.
var ErrForegroundHandoffUnavailable = errors.New("foreground handoff unavailable")

// ErrForegroundHandoffActive reports that a foreground handoff is already
// active and a second launch cannot start.
var ErrForegroundHandoffActive = errors.New("foreground handoff already active")

type foregroundHandoffRequest struct {
	Executable string
	Args       []string
	Env        map[string]string
}

type foregroundHandoffResult struct {
	ExitCode int
}

type foregroundHandoffCall struct {
	ctx    context.Context
	req    foregroundHandoffRequest
	result chan foregroundHandoffCallResult
}

type foregroundHandoffCallResult struct {
	value foregroundHandoffResult
	err   error
}

type foregroundHandoffRunner func(ctx context.Context, req foregroundHandoffRequest) (foregroundHandoffResult, error)

var (
	foregroundRunnerMu sync.RWMutex
	foregroundRunnerID uint64
	foregroundRunner   foregroundHandoffRunner
	foregroundRunnerSN atomic.Uint64
)

func registerForegroundRunner(run foregroundHandoffRunner) func() {
	id := foregroundRunnerSN.Add(1)
	foregroundRunnerMu.Lock()
	foregroundRunnerID = id
	foregroundRunner = run
	foregroundRunnerMu.Unlock()
	return func() {
		foregroundRunnerMu.Lock()
		if foregroundRunnerID == id {
			foregroundRunnerID = 0
			foregroundRunner = nil
		}
		foregroundRunnerMu.Unlock()
	}
}

func runForegroundClient(ctx context.Context, executable string, args []string, env map[string]string) (foregroundHandoffResult, error) {
	foregroundRunnerMu.RLock()
	run := foregroundRunner
	foregroundRunnerMu.RUnlock()
	if run == nil {
		return foregroundHandoffResult{}, ErrForegroundHandoffUnavailable
	}
	return run(ctx, foregroundHandoffRequest{
		Executable: executable,
		Args:       append([]string(nil), args...),
		Env:        cloneEnvMap(env),
	})
}

// RunForegroundClient suspends the active TUI host, runs a child process in
// the same terminal foreground, and resumes the host when the child exits.
func RunForegroundClient(ctx context.Context, executable string, args []string, env map[string]string) (int, error) {
	result, err := runForegroundClient(ctx, executable, args, env)
	if err != nil {
		return 0, err
	}
	return result.ExitCode, nil
}

func (r *Runner[M]) runForegroundHandoff(ctx context.Context, req foregroundHandoffRequest) (foregroundHandoffResult, error) {
	if !r.foregroundInFlight.CompareAndSwap(false, true) {
		return foregroundHandoffResult{}, ErrForegroundHandoffActive
	}
	defer r.foregroundInFlight.Store(false)
	if r.foregroundRequests == nil {
		return foregroundHandoffResult{}, ErrForegroundHandoffUnavailable
	}
	call := foregroundHandoffCall{
		ctx:    ctx,
		req:    req,
		result: make(chan foregroundHandoffCallResult, 1),
	}
	select {
	case <-ctx.Done():
		return foregroundHandoffResult{}, ctx.Err()
	case r.foregroundRequests <- call:
	}
	select {
	case <-ctx.Done():
		return foregroundHandoffResult{}, ctx.Err()
	case response := <-call.result:
		return response.value, response.err
	}
}

func (r *Runner[M]) executeForegroundHandoffCall(call foregroundHandoffCall) {
	result, err := r.executeForegroundHandoff(call.ctx, call.req)
	call.result <- foregroundHandoffCallResult{value: result, err: err}
}

func (r *Runner[M]) executeForegroundHandoff(ctx context.Context, req foregroundHandoffRequest) (foregroundHandoffResult, error) {
	if strings.TrimSpace(req.Executable) == "" { // trimlowerlint:allow boundary canonicalization
		return foregroundHandoffResult{}, errors.New("executable is empty")
	}
	if err := r.Screen.Suspend(); err != nil {
		return foregroundHandoffResult{}, fmt.Errorf("suspend terminal: %w", err)
	}
	defer func() {
		_ = r.Screen.Resume()
		r.previous = nil
		r.Screen.Sync()
		r.Loop.Invalidate()
	}()
	exitCode, err := runForegroundCommand(ctx, req.Executable, req.Args, req.Env)
	if err != nil {
		return foregroundHandoffResult{}, err
	}
	return foregroundHandoffResult{ExitCode: exitCode}, nil
}

var runForegroundCommand = runForegroundCommandOnHost

func runForegroundCommandOnHost(ctx context.Context, executable string, args []string, env map[string]string) (int, error) {
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), envPairs(env)...)
	}
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode(), nil
		}
		return 0, err
	}
	return 0, nil
}

func envPairs(values map[string]string) []string {
	if len(values) == 0 {
		return nil
	}
	pairs := make([]string, 0, len(values))
	for key, value := range values {
		pairs = append(pairs, key+"="+value)
	}
	return pairs
}

func cloneEnvMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}
