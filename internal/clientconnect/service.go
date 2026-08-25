package clientconnect

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
)

type commandRunner func(name string, args ...string) (stdout []byte, exitCode int, err error)

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

func runLocalCommand(name string, args ...string) ([]byte, int, error) {
	cmd := exec.Command(name, args...)
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf
	err := cmd.Run()
	stdout := stdoutBuf.Bytes()
	stderr := stderrBuf.Bytes()
	if err == nil {
		return stdout, 0, nil
	}
	if exitError, ok := err.(*exec.ExitError); ok {
		out := stdout
		if len(bytes.TrimSpace(out)) == 0 && len(bytes.TrimSpace(stderr)) > 0 {
			out = stderr
		}
		return out, exitError.ExitCode(), nil
	}
	return nil, -1, err
}

// Discover returns all clients with a positive presence signal.
func (s *Service) Discover(target Target) []Client {
	if !target.IsLocal() {
		return nil
	}
	var clients []Client
	for _, adapter := range adapters {
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
func (s *Service) Plan(client ClientID, target Target) (Plan, error) {
	if err := target.validateLocal(); err != nil {
		return Plan{}, err
	}
	adapter, ok := adapterFor(client)
	if !ok {
		return Plan{}, fmt.Errorf("unsupported client")
	}
	current, err := adapter.planCurrent(s, target)
	if err != nil {
		return Plan{}, err
	}
	return current.plan.withClient(adapter), nil
}

// Apply re-plans current client state and applies only the semantic mutation
// the operator reviewed. Unrelated human edits are therefore preserved.
func (s *Service) Apply(plan Plan) error {
	if err := plan.Target.validateLocal(); err != nil {
		return err
	}
	adapter, ok := adapterFor(plan.ClientID)
	if !ok {
		return fmt.Errorf("unsupported client")
	}
	current, err := adapter.planCurrent(s, plan.Target)
	if err != nil {
		return err
	}
	current.plan = current.plan.withClient(adapter)
	if current.plan.AlreadyConfigured() {
		return nil
	}
	if !current.plan.equal(plan) {
		return fmt.Errorf("Client configuration changed. Open Connect again to review the current value.")
	}
	if current.apply == nil {
		return fmt.Errorf("client configuration plan has no mutation operation")
	}
	return current.apply()
}
