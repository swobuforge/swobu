package cli_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

func runSwobu(t *testing.T, args ...string) (string, int) {
	t.Helper()

	cmd := exec.Command(swobuBinary(t), args...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return string(out), 0
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("command failed without exit code: %v\n%s", err, string(out))
	}
	return string(out), exitErr.ExitCode()
}

func runSwobuWithEnv(t *testing.T, env map[string]string, args ...string) (string, int) {
	t.Helper()

	cmd := exec.Command(swobuBinary(t), args...)
	cmd.Env = append([]string{}, os.Environ()...)
	for key, value := range env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	out, err := cmd.CombinedOutput()
	if err == nil {
		return string(out), 0
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("command failed without exit code: %v\n%s", err, string(out))
	}
	return string(out), exitErr.ExitCode()
}

var (
	buildOnce   sync.Once
	builtBinary string
	buildErr    error
)

func swobuBinary(t *testing.T) string {
	t.Helper()

	buildOnce.Do(func() {
		repoRoot := repoRootFromWD()
		tempDir, err := os.MkdirTemp("", "swobu-cli-contract-*")
		if err != nil {
			buildErr = err
			return
		}
		builtBinary = filepath.Join(tempDir, "swobu-test-bin")
		cmd := exec.Command("go", "build", "-o", builtBinary, "./cmd/swobu")
		cmd.Dir = repoRoot
		cmd.Env = os.Environ()
		buildErr = cmd.Run()
	})
	if buildErr != nil {
		t.Fatalf("build swobu binary: %v", buildErr)
	}
	return builtBinary
}

func repoRootFromWD() string {
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			panic("could not find repo root from working directory")
		}
		dir = parent
	}
}
