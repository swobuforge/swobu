package cli_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCLI_DaemonStatusAndDownLifecycle(t *testing.T) {
	t.Run("empty endpoint set reports uninitialized and exits 1", func(t *testing.T) {
		addr := reserveLoopbackAddr(t)
		configPath := filepath.Join(t.TempDir(), "swobu.yaml")
		writeConfig(t, configPath, addr, nil)

		daemon := startDaemon(t, configPath)
		defer stopProcess(t, daemon)
		waitForStatus(t, "http://"+addr, 1)

		out, exitCode := runSwobuCLI(t, "status", "--daemon-url", "http://"+addr)
		if exitCode != 1 {
			t.Fatalf("exit code = %d, want 1; out=%s", exitCode, out)
		}
		payload := decodeStatus(t, out)
		if payload.State != "uninitialized" {
			t.Fatalf("state = %q, want uninitialized", payload.State)
		}

		out, exitCode = runSwobuCLI(t, "down", "--daemon-url", "http://"+addr)
		if exitCode != 0 {
			t.Fatalf("down exit code = %d, want 0; out=%s", exitCode, out)
		}
		waitForProcessExit(t, daemon)
	})

	t.Run("configured endpoint set reports healthy and exits 0", func(t *testing.T) {
		addr := reserveLoopbackAddr(t)
		configPath := filepath.Join(t.TempDir(), "swobu.yaml")
		writeConfig(t, configPath, addr, []providerConfigSpec{
			{ref: "backend-a", providerSpec: "custom", protocolKind: "chat_completions", baseURL: "https://example.test/v1"},
		})

		daemon := startDaemon(t, configPath)
		defer stopProcess(t, daemon)
		waitForStatus(t, "http://"+addr, 0)

		out, exitCode := runSwobuCLI(t, "status", "--daemon-url", "http://"+addr)
		if exitCode != 0 {
			t.Fatalf("exit code = %d, want 0; out=%s", exitCode, out)
		}
		payload := decodeStatus(t, out)
		if payload.State != "healthy" {
			t.Fatalf("state = %q, want healthy", payload.State)
		}

		_, _ = runSwobuCLI(t, "down", "--daemon-url", "http://"+addr)
		waitForProcessExit(t, daemon)
	})
}

type statusPayload struct {
	State         string `json:"state"`
	EndpointCount int    `json:"endpoint_count"`
}

type providerConfigSpec struct {
	ref          string
	providerSpec string
	protocolKind string
	baseURL      string
}

func writeConfig(t *testing.T, path string, bindAddr string, providerConfigs []providerConfigSpec) {
	t.Helper()

	var b strings.Builder
	b.WriteString("bind_addr: ")
	b.WriteString(bindAddr)
	b.WriteString("\n")
	b.WriteString("endpoints:\n")
	if len(providerConfigs) == 0 {
		b.WriteString("  []\n")
	} else {
		b.WriteString("  - name: alpha\n")
		b.WriteString("    selected_provider_config_ref: ")
		b.WriteString(providerConfigs[0].ref)
		b.WriteString("\n")
		b.WriteString("    provider_configs:\n")
		for _, providerConfig := range providerConfigs {
			b.WriteString("      - ref: ")
			b.WriteString(providerConfig.ref)
			b.WriteString("\n")
			b.WriteString("        provider_spec: ")
			b.WriteString(providerConfig.providerSpec)
			b.WriteString("\n")
			b.WriteString("        protocol_kind: ")
			b.WriteString(providerConfig.protocolKind)
			b.WriteString("\n")
			if providerConfig.baseURL != "" {
				b.WriteString("        base_url: ")
				b.WriteString(providerConfig.baseURL)
				b.WriteString("\n")
			}
		}
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
}

func decodeStatus(t *testing.T, raw string) statusPayload {
	t.Helper()

	var payload statusPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("status output is not JSON: %v, raw=%q", err, raw)
	}
	return payload
}

func reserveLoopbackAddr(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen returned error: %v", err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()
	return addr
}

func waitForStatus(t *testing.T, daemonURL string, wantExit int) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_, exitCode := runSwobuCLI(t, "status", "--daemon-url", daemonURL)
		if exitCode == wantExit {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("status did not reach exit code %d for %s", wantExit, daemonURL)
}

func waitForProcessExit(t *testing.T, cmd *exec.Cmd) {
	t.Helper()

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) {
				t.Fatalf("daemon process exited with error: %v", err)
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("daemon process did not exit")
	}
}

func stopProcess(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

func startDaemon(t *testing.T, configPath string) *exec.Cmd {
	t.Helper()

	cmd := exec.Command(swobuBinary(t), "daemon", "--config", configPath)
	cmd.Stdout = new(bytes.Buffer)
	cmd.Stderr = new(bytes.Buffer)
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	return cmd
}

func runSwobuCLI(t *testing.T, args ...string) (string, int) {
	t.Helper()

	cmd := exec.Command(swobuBinary(t), args...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return strings.TrimSpace(string(out)), 0
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("command failed without exit code: %v\n%s", err, string(out))
	}
	return strings.TrimSpace(string(out)), exitErr.ExitCode()
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
		tempDir, err := os.MkdirTemp("", "swobu-cli-integration-*")
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
	return filepath.Clean(filepath.Join(wd, "..", "..", ".."))
}
