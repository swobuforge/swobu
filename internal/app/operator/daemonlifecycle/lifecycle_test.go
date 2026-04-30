package daemonlifecycle

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
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
		_, _ = io.WriteString(w, `{"state":"healthy","endpoint_count":1}`)
	}))
	defer srv.Close()

	var stdout bytes.Buffer
	status, err := AttachOrStart(context.Background(), AttachOrStartInput{
		DaemonURL: srv.URL,
		Client:    &http.Client{Timeout: 200 * time.Millisecond},
		Report:    startupReporterForTests(&stdout),
		ResolveDefaultConfig: func() (string, error) {
			return "/tmp/swobu-test-config.yaml", nil
		},
		OpenDaemonLogSink: func() (string, *os.File, error) {
			file, openErr := os.CreateTemp("", "swobu-daemon-log-*.log")
			if openErr != nil {
				return "", nil, openErr
			}
			return file.Name(), file, nil
		},
		SpawnForegroundDaemon: func(context.Context, string, *os.File) error {
			return nil
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
		"|              SWOBU               |",
		"== startup disclosure ==",
		"[CHECKING] daemon not reachable at",
		"[STARTING] starting daemon",
		"[WAITING] waiting for daemon readiness",
		"[READY] daemon ready (healthy)",
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
		_, _ = io.WriteString(w, `{"state":"degraded","endpoint_count":1}`)
	}))
	defer srv.Close()

	calledSpawn := false
	status, err := AttachOrStart(context.Background(), AttachOrStartInput{
		DaemonURL: srv.URL,
		Client:    &http.Client{Timeout: 500 * time.Millisecond},
		Report:    startupReporterForTests(io.Discard),
		SpawnForegroundDaemon: func(context.Context, string, *os.File) error {
			calledSpawn = true
			return nil
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
		DaemonURL: "http://127.0.0.1:1",
		Client:    &http.Client{Timeout: 50 * time.Millisecond},
		Timeout:   100 * time.Millisecond,
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
				_, _ = io.WriteString(w, `{"state":"healthy","endpoint_count":1}`)
				return
			}
			if downRequested.Load() {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"state":"down"}`)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"state":"healthy","endpoint_count":1}`)
		case "/_swobu/down":
			downRequested.Store(true)
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	err := Restart(context.Background(), RestartInput{
		DaemonURL: srv.URL,
		Client:    &http.Client{Timeout: 500 * time.Millisecond},
		ResolveDefaultConfig: func() (string, error) {
			return "/tmp/swobu-test-config.json", nil
		},
		OpenDaemonLogSink: func() (string, *os.File, error) {
			file, err := os.CreateTemp("", "swobu-daemon-log-*.log")
			if err != nil {
				return "", nil, err
			}
			return file.Name(), file, nil
		},
		SpawnForegroundDaemon: func(context.Context, string, *os.File) error {
			if !downRequested.Load() {
				t.Fatalf("spawn called before down request")
			}
			started.Store(true)
			return nil
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
			_, _ = io.WriteString(w, `{"state":"healthy","endpoint_count":1}`)
		case "/_swobu/down":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	err := Restart(context.Background(), RestartInput{
		DaemonURL: srv.URL,
		Client:    &http.Client{Timeout: 500 * time.Millisecond},
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
			_, _ = io.WriteString(w, `{"state":"healthy","endpoint_count":1}`)
		case "/_swobu/down":
			downRequested.Store(true)
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	err := Restart(context.Background(), RestartInput{
		DaemonURL: srv.URL,
		Client:    &http.Client{Timeout: 500 * time.Millisecond},
		ResolveDefaultConfig: func() (string, error) {
			return "/tmp/swobu-test-config.json", nil
		},
		OpenDaemonLogSink: func() (string, *os.File, error) {
			file, openErr := os.CreateTemp("", "swobu-daemon-log-*.log")
			if openErr != nil {
				return "", nil, openErr
			}
			return file.Name(), file, nil
		},
		SpawnForegroundDaemon: func(context.Context, string, *os.File) error {
			return errors.New("spawn failed")
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
		DaemonURL:            "http://127.0.0.1:1",
		Client:               &http.Client{Timeout: 50 * time.Millisecond},
		Report:               startupReporterForTests(&stdout),
		ResolveDefaultConfig: func() (string, error) { return "", errors.New("bad config") },
	})
	if err == nil {
		t.Fatal("AttachOrStart returned nil error, want failure")
	}
	out := stdout.String()
	if !strings.Contains(out, "== startup failed ==") {
		t.Fatalf("stdout missing startup failed block; stdout=%q", out)
	}
	if !strings.Contains(out, "next: run `swobu status`") {
		t.Fatalf("stdout missing next action; stdout=%q", out)
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
		DaemonURL: srv.URL,
		Client:    &http.Client{Timeout: 100 * time.Millisecond},
		Report:    startupReporterForTests(&stdout),
		ResolveDefaultConfig: func() (string, error) {
			return "/tmp/swobu-test-config.yaml", nil
		},
		OpenDaemonLogSink: func() (string, *os.File, error) {
			file, openErr := os.CreateTemp("", "swobu-daemon-log-*.log")
			if openErr != nil {
				return "", nil, openErr
			}
			return file.Name(), file, nil
		},
		SpawnForegroundDaemon: func(context.Context, string, *os.File) error {
			return nil
		},
		ReadinessTimeout: 50 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("AttachOrStart returned nil error, want timeout")
	}
	out := stdout.String()
	if !strings.Contains(out, "== startup timed out ==") {
		t.Fatalf("stdout missing startup timed out block; stdout=%q", out)
	}
	if !strings.Contains(out, "next: run `swobu status`") {
		t.Fatalf("stdout missing timeout next action; stdout=%q", out)
	}
}

func startupReporterForTests(out io.Writer) StartupReporter {
	return startupReporterFunc(func(ev StartupEvent) {
		switch ev.Kind {
		case StartupEventSplash:
			_, _ = io.WriteString(out, "|              SWOBU               |\n")
		case StartupEventDisclosure:
			_, _ = io.WriteString(out, "== startup disclosure ==\n")
		case StartupEventDaemonNotReachable:
			_, _ = io.WriteString(out, fmt.Sprintf("[CHECKING] daemon not reachable at %s\n", ev.DaemonURL))
		case StartupEventStartingDaemon:
			_, _ = io.WriteString(out, "[STARTING] starting daemon\n")
		case StartupEventWaitingReadiness:
			_, _ = io.WriteString(out, "[WAITING] waiting for daemon readiness\n")
		case StartupEventDaemonReady:
			_, _ = io.WriteString(out, fmt.Sprintf("[READY] daemon ready (%s)\n", ev.State))
		case StartupEventStartupFailed:
			_, _ = io.WriteString(out, "== startup failed ==\n")
			for _, next := range ev.NextAction {
				_, _ = io.WriteString(out, "next: "+next+"\n")
			}
		case StartupEventStartupTimedOut:
			_, _ = io.WriteString(out, "== startup timed out ==\n")
			for _, next := range ev.NextAction {
				_, _ = io.WriteString(out, "next: "+next+"\n")
			}
		}
	})
}
