package clientconnect

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
)

type commandRunner func(context.Context, string, ...string) (stdout []byte, exitCode int, err error)

// Service discovers, plans, and applies the closed automatic-client adapter set.
type Service struct {
	homeDir  func() (string, error)
	getenv   func(string) string
	lookPath func(string) (string, error)
	run      commandRunner
}

// NewService returns the process-local client wiring service.
func NewService() *Service {
	return &Service{homeDir: os.UserHomeDir, getenv: os.Getenv, lookPath: exec.LookPath, run: runLocalCommand}
}

func runLocalCommand(ctx context.Context, name string, args ...string) ([]byte, int, error) {
	stdoutFile, err := os.CreateTemp("", "swobu-clientconnect-stdout-*")
	if err != nil {
		return nil, -1, err
	}
	stdoutPath := stdoutFile.Name()
	defer os.Remove(stdoutPath)
	defer stdoutFile.Close()

	stderrFile, err := os.CreateTemp("", "swobu-clientconnect-stderr-*")
	if err != nil {
		return nil, -1, err
	}
	stderrPath := stderrFile.Name()
	defer os.Remove(stderrPath)
	defer stderrFile.Close()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = stdoutFile
	cmd.Stderr = stderrFile
	runErr := cmd.Run()
	if ctx.Err() != nil {
		return nil, -1, ctx.Err()
	}
	if err := stdoutFile.Close(); err != nil {
		return nil, -1, err
	}
	if err := stderrFile.Close(); err != nil {
		return nil, -1, err
	}
	stdout, stdoutErr := os.ReadFile(stdoutPath)
	if stdoutErr != nil {
		return nil, -1, stdoutErr
	}
	stderr, stderrErr := os.ReadFile(stderrPath)
	if stderrErr != nil {
		return nil, -1, stderrErr
	}
	if runErr == nil {
		return stdout, 0, nil
	}
	if exitError, ok := runErr.(*exec.ExitError); ok {
		out := stdout
		if len(bytes.TrimSpace(out)) == 0 && len(bytes.TrimSpace(stderr)) > 0 {
			out = stderr
		}
		return out, exitError.ExitCode(), nil
	}
	return nil, -1, runErr
}

// Discover returns all clients with a positive presence signal.
func (s *Service) Discover(ctx context.Context, target Target) []Client {
	if !target.IsLocal() {
		return nil
	}
	var clients []Client
	for _, adapter := range adapters {
		if ctx.Err() != nil {
			break
		}
		present, err := adapter.present(s)
		if err != nil || !present {
			continue
		}
		clients = append(clients, Client{
			ID:   adapter.id,
			Name: adapter.name,
		})
	}
	return clients
}

// Plan inspects current foreign state and returns its exact semantic delta.
func (s *Service) Plan(ctx context.Context, client ClientID, target Target) (Plan, error) {
	if err := ctx.Err(); err != nil {
		return Plan{}, err
	}
	adapter, ok := adapterFor(client)
	if !ok {
		return Plan{}, fmt.Errorf("unsupported client")
	}
	if err := target.validateLocal(); err != nil {
		return Plan{}, err
	}
	current, err := adapter.planCurrent(ctx, s, target)
	if err != nil {
		return Plan{}, err
	}
	if err := ctx.Err(); err != nil {
		return Plan{}, err
	}
	return current.plan.withClient(adapter), nil
}

// Apply re-plans current client state, applies only the reviewed semantic
// mutation, and returns the authoritative post-operation observation.
func (s *Service) Apply(ctx context.Context, reviewed Plan) (Plan, error) {
	if err := ctx.Err(); err != nil {
		return Plan{}, err
	}
	adapter, ok := adapterFor(reviewed.ClientID)
	if !ok {
		return Plan{}, fmt.Errorf("unsupported client")
	}
	if err := reviewed.Target.validateLocal(); err != nil {
		return Plan{}, err
	}
	current, err := adapter.planCurrent(ctx, s, reviewed.Target)
	if err != nil {
		return Plan{}, err
	}
	current.plan = current.plan.withClient(adapter)
	if current.plan.AlreadyConfigured() {
		return current.plan, nil
	}
	if !current.plan.equal(reviewed) {
		return current.plan, fmt.Errorf("Client configuration changed. Open Connect again to review the current value.")
	}
	if current.apply == nil {
		return current.plan, fmt.Errorf("client configuration plan has no mutation operation")
	}
	if err := ctx.Err(); err != nil {
		return current.plan, err
	}
	applyErr := current.apply(ctx)
	verified, verifyErr := adapter.planCurrent(ctx, s, reviewed.Target)
	if verifyErr == nil {
		verified.plan = verified.plan.withClient(adapter)
	}
	if applyErr != nil {
		if verifyErr == nil {
			return verified.plan, applyErr
		}
		return Plan{}, errors.Join(
			applyErr,
			fmt.Errorf("current client configuration could not be verified: %w", verifyErr),
		)
	}
	if verifyErr != nil {
		return Plan{}, fmt.Errorf("%s configuration was written but could not be verified: %w", adapter.name, verifyErr)
	}
	if !verified.plan.AlreadyConfigured() {
		return verified.plan, fmt.Errorf("%s did not converge to the reviewed configuration", adapter.name)
	}
	return verified.plan, nil
}
