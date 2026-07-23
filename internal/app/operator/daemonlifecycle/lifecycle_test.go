package daemonlifecycle

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAttachOrStart_StartupTranscriptOrder(t *testing.T) {
	t.Parallel()

	var statusCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_swobu/status" {
			http.NotFound(w, r)
			return
		}
		if atomic.AddInt32(&statusCalls, 1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"state":"healthy","workspace_count":1}`)
	}))
	defer srv.Close()

	var stdout bytes.Buffer
	status, err := AttachOrStart(context.Background(), AttachOrStartInput{
		Addr:   strings.TrimPrefix(srv.URL, "http://"),
		Client: &http.Client{Timeout: 200 * time.Millisecond},
		Report: startupReporterForTests(&stdout),
		ResolveConfigPath: func() string {
			return "/tmp/swobu-test-config.yaml"
		},
		SpawnForegroundDaemon: func(context.Context, string, string) (<-chan error, error) {
			return make(chan error), nil
		},
		ReadinessTimeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("AttachOrStart returned error: %v", err)
	}
	if status.State != "healthy" {
		t.Fatalf("state = %q, want healthy", status.State)
	}

	out := stdout.String()
	assertOrderedContains(t, out,
		" ___ ___ ___   __ _____ ___",
		"checking: daemon not reachable at",
		"starting: starting daemon",
		"waiting: waiting for daemon readiness",
		"ready: daemon ready (healthy)",
	)
}

func assertOrderedContains(t *testing.T, text string, tokens ...string) {
	t.Helper()
	start := 0
	for _, token := range tokens {
		pos := bytes.Index([]byte(text[start:]), []byte(token))
		if pos < 0 {
			t.Fatalf("output missing token %q; output=%q", token, text)
		}
		start += pos + len(token)
	}
}

func TestAttachOrStart_AcceptsReachableDegradedState(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_swobu/status" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"state":"degraded","workspace_count":1}`)
	}))
	defer srv.Close()

	calledSpawn := false
	status, err := AttachOrStart(context.Background(), AttachOrStartInput{
		Addr:   strings.TrimPrefix(srv.URL, "http://"),
		Client: &http.Client{Timeout: 500 * time.Millisecond},
		Report: startupReporterForTests(io.Discard),
		SpawnForegroundDaemon: func(context.Context, string, string) (<-chan error, error) {
			calledSpawn = true
			return make(chan error), nil
		},
	})
	if err != nil {
		t.Fatalf("AttachOrStart returned error: %v", err)
	}
	if status.State != "degraded" {
		t.Fatalf("state = %q, want degraded", status.State)
	}
	if calledSpawn {
		t.Fatalf("AttachOrStart spawned daemon despite reachable state")
	}
}

func TestDown_AlreadyStoppedReturnsResult(t *testing.T) {
	t.Parallel()

	result, err := Down(context.Background(), DownInput{
		Addr:    "127.0.0.1:1",
		Client:  &http.Client{Timeout: 50 * time.Millisecond},
		Timeout: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Down returned error: %v", err)
	}
	if result != DownResultAlreadyStopped {
		t.Fatalf("result = %q, want %q", result, DownResultAlreadyStopped)
	}
}

func TestRestart_DownThenAttachStartSucceeds(t *testing.T) {
	t.Parallel()

	var started atomic.Bool
	var downRequested atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/_swobu/status":
			if downRequested.Load() && started.Load() {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"state":"healthy","workspace_count":1}`)
				return
			}
			if downRequested.Load() {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"state":"down"}`)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"state":"healthy","workspace_count":1}`)
		case "/_swobu/down":
			downRequested.Store(true)
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	err := Restart(context.Background(), RestartInput{
		Addr:   strings.TrimPrefix(srv.URL, "http://"),
		Client: &http.Client{Timeout: 500 * time.Millisecond},
		ResolveConfigPath: func() string {
			return "/tmp/swobu-test-config.json"
		},
		SpawnForegroundDaemon: func(context.Context, string, string) (<-chan error, error) {
			if !downRequested.Load() {
				t.Fatalf("spawn called before down request")
			}
			started.Store(true)
			return make(chan error), nil
		},
		ReadinessTimeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("Restart returned error: %v", err)
	}
	if !downRequested.Load() {
		t.Fatal("restart did not request down")
	}
	if !started.Load() {
		t.Fatal("restart did not start daemon")
	}
}

func TestRestart_PropagatesDownFailure(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/_swobu/status":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"state":"healthy","workspace_count":1}`)
		case "/_swobu/down":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	err := Restart(context.Background(), RestartInput{
		Addr:   strings.TrimPrefix(srv.URL, "http://"),
		Client: &http.Client{Timeout: 500 * time.Millisecond},
	})
	if err == nil {
		t.Fatal("Restart returned nil error, want down failure")
	}
}

func TestRestart_PropagatesAttachStartFailureAfterDown(t *testing.T) {
	t.Parallel()

	downRequested := atomic.Bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/_swobu/status":
			if downRequested.Load() {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"state":"down"}`)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"state":"healthy","workspace_count":1}`)
		case "/_swobu/down":
			downRequested.Store(true)
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	err := Restart(context.Background(), RestartInput{
		Addr:   strings.TrimPrefix(srv.URL, "http://"),
		Client: &http.Client{Timeout: 500 * time.Millisecond},
		ResolveConfigPath: func() string {
			return "/tmp/swobu-test-config.json"
		},
		SpawnForegroundDaemon: func(context.Context, string, string) (<-chan error, error) {
			return nil, errors.New("spawn failed")
		},
		ReadinessTimeout: 500 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("Restart returned nil error, want attach/start failure")
	}
}

func TestAttachOrStart_StartupFailureRendersNextActions(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	_, err := AttachOrStart(context.Background(), AttachOrStartInput{
		Addr:              "127.0.0.1:1",
		Client:            &http.Client{Timeout: 50 * time.Millisecond},
		Report:            startupReporterForTests(&stdout),
		ResolveConfigPath: func() string { return "/tmp/swobu-test-config.yaml" },
		SpawnForegroundDaemon: func(context.Context, string, string) (<-chan error, error) {
			return nil, errors.New("bad config")
		},
	})
	if err == nil {
		t.Fatal("AttachOrStart returned nil error, want failure")
	}
	out := stdout.String()
	if !strings.Contains(out, "╭─ startup failed ") {
		t.Fatalf("stdout missing startup failed block; stdout=%q", out)
	}
	if !strings.Contains(out, "next: run `swobu status --addr 127.0.0.1:1`") {
		t.Fatalf("stdout missing next action; stdout=%q", out)
	}
}

func TestAttachOrStart_ChildExitBeforeReadinessReportsExactRecovery(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	addr := strings.TrimPrefix(srv.URL, "http://")
	configPath := "/tmp/swobu locked/config.yaml"
	exited := make(chan error, 1)
	exited <- errors.New("exit status 2")
	var events []StartupEvent
	_, err := AttachOrStart(context.Background(), AttachOrStartInput{
		Addr:              addr,
		Client:            &http.Client{Timeout: 100 * time.Millisecond},
		ResolveConfigPath: func() string { return configPath },
		SpawnForegroundDaemon: func(_ context.Context, gotConfigPath, gotAddr string) (<-chan error, error) {
			if gotConfigPath != configPath || gotAddr != addr {
				t.Fatalf("spawn input = (%q, %q), want (%q, %q)", gotConfigPath, gotAddr, configPath, addr)
			}
			return exited, nil
		},
		Report: startupReporterFunc(func(event StartupEvent) {
			events = append(events, event)
		}),
		ReadinessTimeout: 2 * time.Second,
	})
	if err == nil {
		t.Fatal("AttachOrStart returned nil error, want child exit failure")
	}
	if !strings.Contains(err.Error(), "daemon startup failed") || strings.Contains(err.Error(), "readiness failed") {
		t.Fatalf("error = %q, want startup failure classification", err)
	}
	last := events[len(events)-1]
	if last.Kind != StartupEventStartupFailed {
		t.Fatalf("last event = %q, want startup failure; events=%#v", last.Kind, events)
	}
	if !strings.Contains(last.Text, "daemon exited before readiness") || !strings.Contains(last.Text, "exit status 2") {
		t.Fatalf("failure text = %q, want early child exit", last.Text)
	}
	actions := strings.Join(last.NextAction, "\n")
	if !strings.Contains(actions, fmt.Sprintf("another daemon owns %q", configPath)) ||
		!strings.Contains(actions, fmt.Sprintf("swobu daemon --addr %s --config %q", addr, configPath)) ||
		!strings.Contains(actions, fmt.Sprintf("swobu status --addr %s", addr)) {
		t.Fatalf("recovery actions do not contain resolved values:\n%s", actions)
	}
}

func TestAttachOrStart_StartupTimeoutRendersNextActions(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_swobu/status" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	var stdout bytes.Buffer
	_, err := AttachOrStart(context.Background(), AttachOrStartInput{
		Addr:   strings.TrimPrefix(srv.URL, "http://"),
		Client: &http.Client{Timeout: 100 * time.Millisecond},
		Report: startupReporterForTests(&stdout),
		ResolveConfigPath: func() string {
			return "/tmp/swobu-test-config.yaml"
		},
		SpawnForegroundDaemon: func(context.Context, string, string) (<-chan error, error) {
			return make(chan error), nil
		},
		ReadinessTimeout: 50 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("AttachOrStart returned nil error, want timeout")
	}
	out := stdout.String()
	if !strings.Contains(out, "╭─ startup timed out ") {
		t.Fatalf("stdout missing startup timed out block; stdout=%q", out)
	}
	if !strings.Contains(out, "next: run `swobu status --addr ") {
		t.Fatalf("stdout missing timeout next action; stdout=%q", out)
	}
}

func startupReporterForTests(out io.Writer) StartupReporter {
	return startupReporterFunc(func(ev StartupEvent) {
		switch ev.Kind {
		case StartupEventSplash:
			_, _ = io.WriteString(out, " ___ ___ ___   __ _____ ___\n")
		case StartupEventDaemonNotReachable:
			_, _ = io.WriteString(out, fmt.Sprintf("checking: daemon not reachable at %s\n", ev.Addr))
		case StartupEventStartingDaemon:
			_, _ = io.WriteString(out, "starting: starting daemon\n")
		case StartupEventWaitingReadiness:
			_, _ = io.WriteString(out, "waiting: waiting for daemon readiness\n")
		case StartupEventDaemonReady:
			_, _ = io.WriteString(out, fmt.Sprintf("ready: daemon ready (%s)\n", ev.State))
		case StartupEventStartupFailed:
			_, _ = io.WriteString(out, "╭─ startup failed \n")
			for _, next := range ev.NextAction {
				_, _ = io.WriteString(out, "next: "+next+"\n")
			}
		case StartupEventStartupTimedOut:
			_, _ = io.WriteString(out, "╭─ startup timed out \n")
			for _, next := range ev.NextAction {
				_, _ = io.WriteString(out, "next: "+next+"\n")
			}
		}
	})
}
