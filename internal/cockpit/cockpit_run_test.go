package cockpit

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/creack/pty"
)

func TestRun_NonInteractiveRendersLoadedCockpit(t *testing.T) {
	t.Parallel()

	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/_swobu/workspaces":
			if r.Method != http.MethodGet {
				t.Fatalf("endpoint request method = %s", r.Method)
			}
			called = true
			_, _ = w.Write([]byte(`[{"slug":"dev","default_route":"gpt","route_count":1}]`))
		case "/_swobu/workspaces/dev":
			_, _ = w.Write([]byte(`{"slug":"dev","default_route":"gpt","routes":[{"name":"gpt","tiers":[{"targets":[{"id":"cfg-1","model":"gpt-4.1","protocol":"responses","provider":"custom","connection":{"custom":{"base_url":"https://api.example/v1"}}}]}]}]}`))
		case "/_swobu/status-projection":
			if r.Method != http.MethodGet {
				t.Fatalf("status request method = %s", r.Method)
			}
			_, _ = w.Write([]byte(`{"state":"healthy","recent_traffic":[]}`))
		case "/_swobu/status":
			if r.Method != http.MethodGet {
				t.Fatalf("daemon status request method = %s", r.Method)
			}
			_, _ = w.Write([]byte(`{"state":"healthy","workspace_count":1,"control_plane_protocol":1,"swobu_version":"dev"}`))
		default:
			t.Fatalf("request path = %s", r.URL.Path)
		}
	}))
	defer server.Close()

	var out bytes.Buffer
	err := Run(context.Background(), strings.TrimPrefix(server.URL, "http://"), nil, &out, nil)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !called {
		t.Fatal("Run did not call endpoint control plane")
	}
	for _, want := range []string{"⛉ SWOBU", "[› dev]", server.URL + "/c/dev", "default · 1 target"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("stdout missing %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "placeholder") {
		t.Fatalf("stdout = %q", out.String())
	}
	if !strings.HasSuffix(out.String(), "\n") {
		t.Fatalf("snapshot output should end with newline: %q", out.String())
	}
}

func TestRun_LoadCockpitErrorReturnsError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	var out bytes.Buffer
	err := Run(context.Background(), server.URL, nil, &out, nil)
	if err == nil {
		t.Fatal("Run should return load error")
	}
}

func TestIsInteractiveTerminal_RequiresBothStdinAndStdoutTerminals(t *testing.T) {
	t.Parallel()

	ptmx, tty, err := pty.Open()
	if err != nil {
		t.Fatalf("open pty: %v", err)
	}
	t.Cleanup(func() {
		_ = ptmx.Close()
		_ = tty.Close()
	})
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("open pipe: %v", err)
	}
	t.Cleanup(func() {
		_ = r.Close()
		_ = w.Close()
	})

	if !isInteractiveTerminal(tty, tty) {
		t.Fatal("pty stdin and stdout should be interactive")
	}
	if isInteractiveTerminal(tty, &bytes.Buffer{}) {
		t.Fatal("pty stdin with wrapped stdout should not be interactive")
	}
	if isInteractiveTerminal(r, tty) {
		t.Fatal("pipe stdin with pty stdout should not be interactive")
	}
}

func TestStopCockpitOnContextExitsWithRetiredApp(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	appStopped := make(chan struct{})
	watcherDone := make(chan struct{})
	stopCalls := make(chan struct{}, 1)
	go func() {
		defer close(watcherDone)
		stopCockpitOnContext(ctx, appStopped, func() { stopCalls <- struct{}{} })
	}()

	close(appStopped)
	<-watcherDone
	cancel()
	select {
	case <-stopCalls:
		t.Fatal("retired app watcher reacted to later root cancellation")
	default:
	}
}

func TestStopCockpitOnContextStopsLiveAppOnRootCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	appStopped := make(chan struct{})
	stopCalls := make(chan struct{}, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		stopCockpitOnContext(ctx, appStopped, func() { stopCalls <- struct{}{} })
	}()

	cancel()
	<-done
	select {
	case <-stopCalls:
	default:
		t.Fatal("root cancellation did not stop the live app")
	}
}
