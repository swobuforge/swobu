package cli_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestB070_DaemonNamespaceContract(t *testing.T) {
	t.Run("status returns down JSON and exit 2 when daemon is unreachable", func(t *testing.T) {
		out, exitCode := runSwobu(t, "status", "--daemon-url", "http://127.0.0.1:1")
		if exitCode != 2 {
			t.Fatalf("exit code = %d, want 2; out=%s", exitCode, out)
		}
		payload := decodeStatusPayload(t, out)
		if payload.State != "down" {
			t.Fatalf("state = %q, want down", payload.State)
		}
	})

	t.Run("status returns uninitialized JSON and exit 1 when daemon is reachable but empty", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet || r.URL.Path != "/_swobu/status" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"state":                  "uninitialized",
				"endpoint_count":         0,
				"control_plane_protocol": 7,
				"swobu_version":          "0.9.0",
			})
		}))
		defer srv.Close()

		out, exitCode := runSwobu(t, "status", "--daemon-url", srv.URL)
		if exitCode != 1 {
			t.Fatalf("exit code = %d, want 1; out=%s", exitCode, out)
		}
		payload := decodeStatusPayload(t, out)
		if payload.State != "uninitialized" {
			t.Fatalf("state = %q, want uninitialized", payload.State)
		}
		if payload.SwobuVersion != "0.9.0" {
			t.Fatalf("swobu_version = %q, want 0.9.0", payload.SwobuVersion)
		}
	})

	t.Run("status returns healthy JSON and exit 0 when daemon is healthy", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet || r.URL.Path != "/_swobu/status" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"state":                  "healthy",
				"endpoint_count":         1,
				"control_plane_protocol": 7,
				"swobu_version":          "0.9.0",
			})
		}))
		defer srv.Close()

		out, exitCode := runSwobu(t, "status", "--daemon-url", srv.URL)
		if exitCode != 0 {
			t.Fatalf("exit code = %d, want 0; out=%s", exitCode, out)
		}
		payload := decodeStatusPayload(t, out)
		if payload.State != "healthy" {
			t.Fatalf("state = %q, want healthy", payload.State)
		}
		if payload.SwobuVersion != "0.9.0" {
			t.Fatalf("swobu_version = %q, want 0.9.0", payload.SwobuVersion)
		}
	})

	t.Run("status returns degraded JSON and exit 1 when daemon is reachable but degraded", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet || r.URL.Path != "/_swobu/status" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"state":                  "degraded",
				"endpoint_count":         1,
				"control_plane_protocol": 7,
				"swobu_version":          "0.9.0",
			})
		}))
		defer srv.Close()

		out, exitCode := runSwobu(t, "status", "--daemon-url", srv.URL)
		if exitCode != 1 {
			t.Fatalf("exit code = %d, want 1; out=%s", exitCode, out)
		}
		payload := decodeStatusPayload(t, out)
		if payload.State != "degraded" {
			t.Fatalf("state = %q, want degraded", payload.State)
		}
		if payload.SwobuVersion != "0.9.0" {
			t.Fatalf("swobu_version = %q, want 0.9.0", payload.SwobuVersion)
		}
	})

	t.Run("down requests graceful shutdown and waits for stop", func(t *testing.T) {
		var stopped bool
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodPost && r.URL.Path == "/_swobu/down":
				stopped = true
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"ok":true}`))
			case r.Method == http.MethodGet && r.URL.Path == "/_swobu/status" && !stopped:
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"state":                  "healthy",
					"endpoint_count":         1,
					"control_plane_protocol": 7,
					"swobu_version":          "0.9.0",
				})
			default:
				http.NotFound(w, r)
			}
		}))
		defer srv.Close()

		out, exitCode := runSwobu(t, "down", "--daemon-url", srv.URL)
		if exitCode != 0 {
			t.Fatalf("exit code = %d, want 0; out=%s", exitCode, out)
		}
		if out != "" {
			t.Fatalf("stdout = %q, want empty", out)
		}
		if !stopped {
			t.Fatal("down did not trigger shutdown request")
		}
	})

	t.Run("down returns success with already stopped message when daemon is unreachable", func(t *testing.T) {
		out, exitCode := runSwobu(t, "down", "--daemon-url", "http://127.0.0.1:1")
		if exitCode != 0 {
			t.Fatalf("exit code = %d, want 0; out=%s", exitCode, out)
		}
		if payload := strings.TrimSpace(out); payload != "daemon already stopped" {
			t.Fatalf("output = %q, want %q", payload, "daemon already stopped")
		}
	})
}

type statusPayload struct {
	State                string `json:"state"`
	EndpointCount        int    `json:"endpoint_count"`
	ControlPlaneProtocol int    `json:"control_plane_protocol"`
	SwobuVersion         string `json:"swobu_version"`
}

func decodeStatusPayload(t *testing.T, raw string) statusPayload {
	t.Helper()

	var payload statusPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("status output is not JSON: %v, raw=%q", err, raw)
	}
	return payload
}
