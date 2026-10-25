package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/swobuforge/swobu/internal/bootstrap"
)

func TestDaemonStartupSelectionPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name, addr, config   string
		args                 []string
		wantAddr, wantConfig string
	}{
		{"environment", "", "", nil, "127.0.0.1:19001", "environment.yaml"},
		{"runner", "127.0.0.1:19002", "runner.yaml", nil, "127.0.0.1:19002", "runner.yaml"},
		{"leaf", "127.0.0.1:19002", "runner.yaml", []string{"--addr", "127.0.0.1:19003", "--config", "leaf.yaml"}, "127.0.0.1:19003", "leaf.yaml"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SWOBU_HOME", t.TempDir())
			t.Setenv("SWOBU_ADDR", "127.0.0.1:19001")
			t.Setenv("SWOBU_CONFIG_PATH", "environment.yaml")
			var out, notices strings.Builder
			var captured *bootstrap.StartInput
			runner := Runner{Addr: tc.addr, ConfigPath: tc.config, Stdout: &out, Stderr: &notices,
				Start: func(_ context.Context, input bootstrap.StartInput) (*bootstrap.Daemon, error) {
					captured = &input
					return nil, errors.New("stop at startup boundary")
				},
			}
			if code := runner.Run(t.Context(), append([]string{"daemon"}, tc.args...)); code != ExitDown || captured == nil {
				t.Fatalf("startup boundary not reached: code=%d stderr=%q", code, notices.String())
			}
			if captured.StartupConfig.Addr != tc.wantAddr || captured.ConfigPath != tc.wantConfig {
				t.Fatalf("startup selection: addr=%q config=%q; want %q %q", captured.StartupConfig.Addr, captured.ConfigPath, tc.wantAddr, tc.wantConfig)
			}
		})
	}
}

// A Runner's chosen address must be respected by every local Control command,
// not only by `swobu open`. Environment fallbacks belong below that choice.
func TestRunnerSelectedNodeTargetsReadCommands(t *testing.T) {
	var wrongCalls atomic.Int32
	wrong := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wrongCalls.Add(1)
		t.Errorf("command escaped selected local Node: %s %s", r.Method, r.URL.Path)
		http.Error(w, "wrong Node", http.StatusNotFound)
	}))
	defer wrong.Close()
	t.Setenv("SWOBU_ADDR", strings.TrimPrefix(wrong.URL, "http://"))

	var statusCalls, principalCalls, inspectCalls, previewCalls atomic.Int32
	selected := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/status":
			statusCalls.Add(1)
			fmt.Fprint(w, `{"state":"healthy","control_plane_protocol":11}`)
		case "/api/v1/principals":
			principalCalls.Add(1)
			fmt.Fprint(w, `[{"id":"acc_1","label":"Selected","kind":"managed","status":"active","permissions":[]}]`)
		case "/api/v1/telemetry/report":
			inspectCalls.Add(1)
			fmt.Fprint(w, `{"schema":1,"traffic":[]}`)
		case "/api/v1/client-configurations":
			if r.Method != http.MethodPost || r.URL.RawQuery != "dry_run=true" {
				t.Errorf("Connect escaped preview: %s %s", r.Method, r.URL)
			}
			previewCalls.Add(1)
			fmt.Fprint(w, `{"workspace":"research","model_id":"coding","client":{"id":"codex","name":"Codex"},"authentication":{"principal":"acc_AAAAAAAAAAAAAAAAAAAAAA","permission_revision":"rev-1","issue_credential":false,"credential_id":"acred_AAAAAAAAAAAAAAAAAAAAAA"},"config_paths":[],"changes":[],"already_configured":true,"requires_replace":false}`)
		default:
			t.Errorf("unexpected selected-Node operation: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer selected.Close()

	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"status", []string{"status"}, `"state":"healthy"`},
		{"principal list", []string{"principal", "list"}, "acc_1"},
		{"telemetry inspect", []string{"telemetry", "inspect"}, `"schema":1`},
		{"Connect dry run", []string{"connect", "codex", "--workspace", "research", "--model", "coding", "--principal", "acc_AAAAAAAAAAAAAAAAAAAAAA", "--dry-run"}, "Nothing applied"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr strings.Builder
			r := Runner{Addr: strings.TrimPrefix(selected.URL, "http://"), HTTPClient: selected.Client(), Stdout: &stdout, Stderr: &stderr}
			if code := r.Run(t.Context(), tc.args); code != ExitHealthy || !strings.Contains(stdout.String(), tc.want) {
				t.Fatalf("code=%d output=%q errors=%q", code, stdout.String(), stderr.String())
			}
		})
	}
	if wrongCalls.Load() != 0 || statusCalls.Load() != 3 || principalCalls.Load() != 1 || inspectCalls.Load() != 1 || previewCalls.Load() != 1 {
		t.Fatalf("target leakage: wrong=%d status=%d principal=%d inspect=%d preview=%d", wrongCalls.Load(), statusCalls.Load(), principalCalls.Load(), inspectCalls.Load(), previewCalls.Load())
	}
}

func TestPrincipalListRejectsIgnoredInputAndEscapesTerminalControls(t *testing.T) {
	var calls atomic.Int32
	selected := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.URL.Path {
		case "/api/v1/status":
			fmt.Fprint(w, `{"state":"healthy","control_plane_protocol":11}`)
		case "/api/v1/principals":
			fmt.Fprint(w, `[{"id":"acc_1","label":"Alice\n\u001b[31madmin","kind":"managed","status":"active","permissions":[{"workspace":"research","action":"use"}]}]`)
		default:
			t.Errorf("unexpected request %s", r.URL)
		}
	}))
	defer selected.Close()
	address := strings.TrimPrefix(selected.URL, "http://")

	for _, args := range [][]string{
		{"principal", "list", "surprise"},
		{"principal", "list", "--unknown"},
		{"principal", "list", "--addr", "example.com:9921"},
	} {
		var stdout, stderr strings.Builder
		r := Runner{Addr: address, HTTPClient: selected.Client(), Stdout: &stdout, Stderr: &stderr}
		if code := r.Run(t.Context(), args); code != ExitDown || stdout.Len() != 0 || calls.Load() != 0 {
			t.Fatalf("invalid command reached Node: %v code=%d stdout=%q stderr=%q calls=%d", args, code, stdout.String(), stderr.String(), calls.Load())
		}
	}

	var human, notices strings.Builder
	if code := (Runner{Addr: address, HTTPClient: selected.Client(), Stdout: &human, Stderr: &notices}).Run(t.Context(), []string{"principal", "list"}); code != ExitHealthy {
		t.Fatalf("human list failed: code=%d stderr=%q", code, notices.String())
	}
	if strings.Contains(human.String(), "\x1b") || strings.Count(human.String(), "\n") != 2 || !strings.Contains(human.String(), `Alice\n\x1b[31madmin`) {
		t.Fatalf("terminal injection reached table: %q", human.String())
	}

	var machine strings.Builder
	if code := (Runner{Addr: address, HTTPClient: selected.Client(), Stdout: &machine, Stderr: &notices}).Run(t.Context(), []string{"principal", "list", "--json"}); code != ExitHealthy {
		t.Fatalf("JSON list failed: code=%d stderr=%q", code, notices.String())
	}
	var entries []struct {
		Label string `json:"label"`
	}
	if err := json.Unmarshal([]byte(machine.String()), &entries); err != nil || len(entries) != 1 || entries[0].Label != "Alice\n\x1b[31madmin" {
		t.Fatalf("lossy JSON Principal projection: %q, %v", machine.String(), err)
	}
}

func TestRunnerExplicitLeafAddrOverridesDefault(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/status" {
			t.Errorf("unexpected request: %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"state":"healthy","control_plane_protocol":11}`)
	}))
	defer server.Close()
	var output, notices strings.Builder
	r := Runner{Addr: "invalid.example:9000", HTTPClient: server.Client(), Stdout: &output, Stderr: &notices}
	if code := r.Run(t.Context(), []string{"status", "--addr", strings.TrimPrefix(server.URL, "http://")}); code != ExitHealthy {
		t.Fatalf("leaf override lost: code=%d stderr=%q", code, notices.String())
	}
}

func TestRunnerVersionRejectsExtraArguments(t *testing.T) {
	var output, errors strings.Builder
	r := Runner{Stdout: &output, Stderr: &errors}
	if code := r.Run(t.Context(), []string{"version", "ignored"}); code != ExitDown || output.Len() != 0 {
		t.Fatalf("ignored version positional argument: code=%d out=%q", code, output.String())
	}
}

// Shutdown is an irreversible local Control effect. It must never fall back to
// SWOBU_ADDR when a caller explicitly selected a different Node.
func TestRunnerShutdownTargetsSelectedNode(t *testing.T) {
	var wrongCalls, shutdowns atomic.Int32
	wrong := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wrongCalls.Add(1)
		http.Error(w, "wrong Node", http.StatusForbidden)
	}))
	defer wrong.Close()
	t.Setenv("SWOBU_ADDR", strings.TrimPrefix(wrong.URL, "http://"))

	selected := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/status":
			if shutdowns.Load() == 0 {
				fmt.Fprint(w, `{"state":"healthy","control_plane_protocol":11}`)
			} else {
				w.WriteHeader(http.StatusServiceUnavailable)
			}
		case "/api/v1/daemon/shutdown":
			if r.Method != http.MethodPost {
				t.Errorf("shutdown used %s", r.Method)
			}
			shutdowns.Add(1)
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected local daemon operation: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer selected.Close()
	var stdout, stderr strings.Builder
	r := Runner{Addr: strings.TrimPrefix(selected.URL, "http://"), HTTPClient: selected.Client(), Stdout: &stdout, Stderr: &stderr}
	if code := r.Run(t.Context(), []string{"daemon", "down", "--timeout", "1s"}); code != ExitHealthy {
		t.Fatalf("local selected shutdown failed: code=%d stderr=%q", code, stderr.String())
	}
	if wrongCalls.Load() != 0 || shutdowns.Load() != 1 {
		t.Fatalf("wrong shutdown destination: wrong=%d selected=%d", wrongCalls.Load(), shutdowns.Load())
	}
}

func TestRunnerStatusRejectsNonLocalAddressWithActionableError(t *testing.T) {
	var stdout, stderr strings.Builder
	r := Runner{Addr: "remote.example:7926", Stdout: &stdout, Stderr: &stderr}
	if code := r.Run(t.Context(), []string{"status"}); code != ExitDown || stdout.Len() != 0 || !strings.Contains(stderr.String(), "must be loopback") {
		t.Fatalf("non-local status target not safely rejected: code=%d out=%q err=%q", code, stdout.String(), stderr.String())
	}
}
