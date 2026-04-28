// Package harness provides process and terminal mechanics for Swobu E2E tests.
package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/metrofun/swobu/internal/domain/endpointintent"
	"github.com/metrofun/swobu/internal/domain/protocolsurface"
)

type DaemonStatusPayload struct {
	State                string `json:"state"`
	EndpointCount        int    `json:"endpoint_count"`
	ControlPlaneProtocol int    `json:"control_plane_protocol"`
	SwobuVersion         string `json:"swobu_version"`
}

type DaemonProcessHarness struct {
	BaseURL    string
	daemonURL  string
	binaryPath string
	cmd        *exec.Cmd
	stdout     *lockedWriterBuffer
	stderr     *lockedWriterBuffer
}

type DaemonProcessConfig struct {
	Endpoints []endpointintent.Endpoint
	BindAddr  string
}

func StartDaemonProcess(t *testing.T, cfg DaemonProcessConfig) DaemonProcessHarness {
	t.Helper()

	bindAddr := strings.TrimSpace(cfg.BindAddr)
	if bindAddr == "" {
		bindAddr = "127.0.0.1:0"
	}
	configPath := filepath.Join(t.TempDir(), "swobu.yaml")
	if err := os.WriteFile(configPath, []byte(renderRuntimeConfigYAML(cfg.Endpoints, bindAddr)), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	stdout := &lockedWriterBuffer{}
	stderr := &lockedWriterBuffer{}
	cmd := exec.Command(swobuBinaryPath(t), "daemon", "--config", configPath)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	harness := DaemonProcessHarness{
		binaryPath: swobuBinaryPath(t),
		cmd:        cmd,
		stdout:     stdout,
		stderr:     stderr,
	}
	effectiveAddr := bindAddr
	if bindAddr == "127.0.0.1:0" {
		effectiveAddr = harness.waitForBoundAddr(t)
	}
	harness.BaseURL = "http://" + effectiveAddr
	harness.daemonURL = "http://" + effectiveAddr

	allowedExitCodes := []int{0}
	if len(cfg.Endpoints) == 0 {
		allowedExitCodes = append(allowedExitCodes, 1)
	}
	harness.waitForStatusAny(t, allowedExitCodes...)
	if len(cfg.Endpoints) > 0 {
		harness.waitForEndpointList(t, len(cfg.Endpoints))
	}
	t.Cleanup(harness.Close)
	return harness
}

func SwobuBinaryPath(t *testing.T) string {
	t.Helper()
	return swobuBinaryPath(t)
}

func (h DaemonProcessHarness) Close() {
	if h.cmd == nil || h.cmd.Process == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	down := exec.CommandContext(ctx, h.binaryPath, "down", "--daemon-url", h.daemonURL)
	_ = down.Run()

	done := make(chan error, 1)
	go func() { done <- h.cmd.Wait() }()
	select {
	case <-ctx.Done():
		_ = h.cmd.Process.Kill()
		<-done
	case <-done:
	}
}

func (h DaemonProcessHarness) Status() (DaemonStatusPayload, int, error) {
	cmd := exec.Command(h.binaryPath, "status", "--daemon-url", h.daemonURL)
	out, err := cmd.CombinedOutput()
	if err == nil {
		var payload DaemonStatusPayload
		if decErr := json.Unmarshal(bytes.TrimSpace(out), &payload); decErr != nil {
			return DaemonStatusPayload{}, 0, decErr
		}
		return payload, 0, nil
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return DaemonStatusPayload{}, 0, err
	}
	var payload DaemonStatusPayload
	if decErr := json.Unmarshal(bytes.TrimSpace(out), &payload); decErr != nil {
		return DaemonStatusPayload{}, exitErr.ExitCode(), decErr
	}
	return payload, exitErr.ExitCode(), nil
}

func (h DaemonProcessHarness) waitForStatusAny(testingT *testing.T, wantExitCodes ...int) {
	testingT.Helper()
	if len(wantExitCodes) == 0 {
		wantExitCodes = []int{0}
	}
	allowed := make(map[int]struct{}, len(wantExitCodes))
	for _, code := range wantExitCodes {
		allowed[code] = struct{}{}
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		payload, exitCode, err := h.Status()
		if err == nil {
			if _, ok := allowed[exitCode]; ok {
				_ = payload
				return
			}
		}
		if h.cmd.ProcessState != nil && h.cmd.ProcessState.Exited() {
			testingT.Fatalf("daemon exited early; stdout=%s stderr=%s", h.stdout.String(), h.stderr.String())
			_ = payload
		}
		time.Sleep(50 * time.Millisecond)
	}
	testingT.Fatalf("daemon did not reach any exit code %v; stdout=%s stderr=%s", wantExitCodes, h.stdout.String(), h.stderr.String())
}

func (h DaemonProcessHarness) waitForEndpointList(testingT *testing.T, wantMinCount int) {
	testingT.Helper()

	if wantMinCount <= 0 {
		return
	}
	client := &http.Client{Timeout: 500 * time.Millisecond}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, h.daemonURL+"/_swobu/endpoints", nil)
		if err == nil {
			resp, err := client.Do(req)
			if err == nil {
				var payload struct {
					Endpoints []json.RawMessage `json:"endpoints"`
				}
				decodeErr := json.NewDecoder(resp.Body).Decode(&payload)
				_ = resp.Body.Close()
				if resp.StatusCode == http.StatusOK && decodeErr == nil && len(payload.Endpoints) >= wantMinCount {
					return
				}
			}
		}
		if h.cmd.ProcessState != nil && h.cmd.ProcessState.Exited() {
			testingT.Fatalf("daemon exited early; stdout=%s stderr=%s", h.stdout.String(), h.stderr.String())
		}
		time.Sleep(50 * time.Millisecond)
	}
	testingT.Fatalf("daemon endpoint list did not reach %d entries; stdout=%s stderr=%s", wantMinCount, h.stdout.String(), h.stderr.String())
}

var (
	bindAddrKVPattern   = regexp.MustCompile(`\bbind_addr=([0-9.:]+)`)
	bindAddrJSONPattern = regexp.MustCompile(`"bind_addr":"([^"]+)"`)
)

func (h DaemonProcessHarness) waitForBoundAddr(testingT *testing.T) string {
	testingT.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		combined := h.stdout.String() + "\n" + h.stderr.String()
		if addr := parseBoundAddr(combined); addr != "" {
			return addr
		}
		if h.cmd.ProcessState != nil && h.cmd.ProcessState.Exited() {
			testingT.Fatalf("daemon exited before reporting bound addr; stdout=%s stderr=%s", h.stdout.String(), h.stderr.String())
		}
		time.Sleep(25 * time.Millisecond)
	}
	testingT.Fatalf("daemon did not report bound addr; stdout=%s stderr=%s", h.stdout.String(), h.stderr.String())
	return ""
}

func parseBoundAddr(output string) string {
	output = strings.TrimSpace(output)
	if output == "" {
		return ""
	}
	for _, match := range bindAddrKVPattern.FindAllStringSubmatch(output, -1) {
		if len(match) < 2 {
			continue
		}
		addr := strings.TrimSpace(match[1])
		if addr != "" && !strings.HasSuffix(addr, ":0") {
			return addr
		}
	}
	for _, match := range bindAddrJSONPattern.FindAllStringSubmatch(output, -1) {
		if len(match) < 2 {
			continue
		}
		addr := strings.TrimSpace(match[1])
		if addr != "" && !strings.HasSuffix(addr, ":0") {
			return addr
		}
	}
	return ""
}

type lockedWriterBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedWriterBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedWriterBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func NewEndpoint(
	t *testing.T,
	name string,
	selectedRef string,
	providerConfigs ...endpointintent.ProviderConfig,
) endpointintent.Endpoint {
	t.Helper()

	parsedName, err := endpointintent.ParseEndpointName(name)
	if err != nil {
		t.Fatalf("ParseEndpointName returned error: %v", err)
	}
	selected, err := endpointintent.ParseProviderConfigRef(selectedRef)
	if err != nil {
		t.Fatalf("ParseProviderConfigRef returned error: %v", err)
	}
	endpoint, err := endpointintent.NewEndpoint(parsedName, providerConfigs, selected)
	if err != nil {
		t.Fatalf("NewEndpoint returned error: %v", err)
	}
	return endpoint
}

func NewProviderConfig(
	t *testing.T,
	ref string,
	providerSpec string,
	baseURL string,
	credentialRef string,
	protocol protocolsurface.Kind,
) endpointintent.ProviderConfig {
	t.Helper()

	parsedRef, err := endpointintent.ParseProviderConfigRef(ref)
	if err != nil {
		t.Fatalf("ParseProviderConfigRef returned error: %v", err)
	}
	parsedSpec, err := endpointintent.ParseProviderSpec(providerSpec)
	if err != nil {
		t.Fatalf("ParseProviderSpec returned error: %v", err)
	}
	providerConfig, err := endpointintent.NewProviderConfig(parsedRef, parsedSpec, baseURL, credentialRef, protocol)
	if err != nil {
		t.Fatalf("NewProviderConfig returned error: %v", err)
	}
	return providerConfig
}

func renderRuntimeConfigYAML(endpoints []endpointintent.Endpoint, bindAddr string) string {
	var b strings.Builder
	b.WriteString("bind_addr: ")
	b.WriteString(bindAddr)
	b.WriteString("\n")
	b.WriteString("endpoints:\n")
	if len(endpoints) == 0 {
		b.WriteString("  []\n")
		return b.String()
	}
	for _, endpoint := range endpoints {
		fmt.Fprintf(&b, "  - name: %s\n", endpoint.Name().String())
		fmt.Fprintf(&b, "    selected_provider_config_ref: %s\n", endpoint.SelectedProviderConfigRef().String())
		b.WriteString("    provider_configs:\n")
		for _, providerConfig := range endpoint.ProviderConfigs() {
			fmt.Fprintf(&b, "      - ref: %s\n", providerConfig.Ref().String())
			fmt.Fprintf(&b, "        provider_spec: %s\n", providerConfig.ProviderSpec().String())
			fmt.Fprintf(&b, "        protocol_kind: %s\n", providerConfig.ProtocolKind().String())
			if providerConfig.ModelID() != "" {
				fmt.Fprintf(&b, "        model_id: %s\n", providerConfig.ModelID())
			}
			if providerConfig.TargetAlias() != "" {
				fmt.Fprintf(&b, "        target_alias: %s\n", providerConfig.TargetAlias())
			}
			if providerConfig.BaseURL() != "" {
				fmt.Fprintf(&b, "        base_url: %s\n", providerConfig.BaseURL())
			}
			if providerConfig.CredentialRef() != "" {
				fmt.Fprintf(&b, "        credential_ref: %s\n", providerConfig.CredentialRef())
			}
		}
	}
	return b.String()
}

var (
	buildOnce   sync.Once
	builtBinary string
	buildErr    error
)

func swobuBinaryPath(t *testing.T) string {
	t.Helper()

	buildOnce.Do(func() {
		tempDir, err := os.MkdirTemp("", "swobu-e2e-*")
		if err != nil {
			buildErr = err
			return
		}
		builtBinary = filepath.Join(tempDir, "swobu-test-bin")
		cmd := exec.Command("go", "build", "-o", builtBinary, "./cmd/swobu")
		cmd.Dir = repoRootFromWD()
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		output, runErr := cmd.CombinedOutput()
		if runErr != nil {
			buildErr = fmt.Errorf("%w: %s", runErr, strings.TrimSpace(string(output)))
			return
		}
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
