package bootstrap

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/app/operator/chatgptlogin"
	platformconfig "github.com/swobuforge/swobu/internal/platform/config"
)

func TestDaemonCloseCancelsBrowserAuthAndReleasesCallbackListener(t *testing.T) {
	login := chatgptlogin.NewService(http.DefaultClient, chatgptlogin.ServiceConfig{CallbackListenAddr: "127.0.0.1:0"})
	started, err := login.Start(context.Background(), chatgptlogin.StartInput{AuthMode: "browser"})
	if err != nil {
		t.Fatalf("start browser session: %v", err)
	}
	authorizeURL, err := url.Parse(started.AuthorizeURL)
	if err != nil {
		t.Fatalf("parse authorize URL: %v", err)
	}
	callback, err := url.Parse(authorizeURL.Query().Get("redirect_uri"))
	if err != nil || callback.Port() == "" {
		t.Fatalf("parse callback URL: url=%q error=%v", authorizeURL.Query().Get("redirect_uri"), err)
	}
	callbackAddr := net.JoinHostPort("127.0.0.1", callback.Port())

	serveDone := make(chan struct{})
	close(serveDone)
	daemon := &Daemon{
		server:       &http.Server{},
		serveDone:    serveDone,
		logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		chatGPTLogin: login,
	}
	if err := daemon.Close(); err != nil {
		t.Fatalf("close daemon: %v", err)
	}
	status, err := login.Session(context.Background(), started.SessionID)
	if err != nil {
		t.Fatalf("read shutdown session: %v", err)
	}
	if status.State != chatgptlogin.SessionCanceled {
		t.Fatalf("shutdown session state=%q, want %q", status.State, chatgptlogin.SessionCanceled)
	}
	listener, err := net.Listen("tcp", callbackAddr)
	if err != nil {
		t.Fatalf("callback listener remains bound after daemon close: %v", err)
	}
	_ = listener.Close()
	if _, err := login.Start(context.Background(), chatgptlogin.StartInput{AuthMode: "browser"}); err == nil || !strings.Contains(err.Error(), "shut down") {
		t.Fatalf("login start after daemon close error=%v", err)
	}
}

func TestDaemonAuthStartMarkerSurvivesDaemonRestart(t *testing.T) {
	t.Setenv("DO_NOT_TRACK", "1")
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(directory, "swobu.yaml")
	if err := os.WriteFile(configPath, []byte("schema_version: 5\nworkspaces: {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	startDaemon := func() *Daemon {
		d, err := Start(context.Background(), StartInput{
			ConfigPath:    configPath,
			StartupConfig: platformconfig.StartupConfig{Addr: "127.0.0.1:0"},
			Logger:        logger,
		})
		if err != nil {
			t.Fatalf("start daemon: %v", err)
		}
		return d
	}

	first := startDaemon()
	request := func(d *Daemon) (int, string) {
		body := `{"provider_spec":"chatgpt","draft_subject":"draft","auth_mode":"browser"}`
		req, err := http.NewRequest(http.MethodPost, d.BaseURL()+"/api/v1/auth-sessions", strings.NewReader(body))
		if err != nil {
			t.Fatalf("build auth start request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", "compiled-restart-auth-key")
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("send auth start request: %v", err)
		}
		defer response.Body.Close()
		payload, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatalf("read auth start response: %v", err)
		}
		return response.StatusCode, string(payload)
	}
	firstStatus, firstBody := request(first)
	if firstStatus != http.StatusCreated {
		_ = first.Close()
		t.Fatalf("first auth start status=%d body=%s", firstStatus, firstBody)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first daemon: %v", err)
	}

	second := startDaemon()
	defer second.Close()
	secondStatus, secondBody := request(second)
	if secondStatus != http.StatusConflict || !strings.Contains(secondBody, "AUTH_START_INDETERMINATE") {
		t.Fatalf("reopened auth start status=%d body=%s", secondStatus, secondBody)
	}
	entries, err := os.ReadDir(filepath.Join(directory, "auth-start-attempts"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("retained attempts=%v error=%v", entries, err)
	}
}

func TestDaemonAuthStartCrashHelper(t *testing.T) {
	configPath := os.Getenv("SWOBU_DAEMON_AUTH_START_CRASH_CONFIG")
	if configPath == "" {
		return
	}
	readyPath := os.Getenv("SWOBU_DAEMON_AUTH_START_CRASH_READY")
	if readyPath == "" {
		t.Fatal("child daemon handoff ready path is required")
	}
	if err := os.WriteFile(readyPath, []byte("starting daemon\n"), 0600); err != nil {
		t.Fatalf("publish child daemon startup: %v", err)
	}
	d, err := Start(context.Background(), StartInput{
		ConfigPath:    configPath,
		StartupConfig: platformconfig.StartupConfig{Addr: "127.0.0.1:0"},
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("start child daemon: %v", err)
	}
	if err := os.WriteFile(readyPath, []byte("daemon started\n"), 0600); err != nil {
		t.Fatalf("publish child daemon started: %v", err)
	}
	request, err := http.NewRequest(http.MethodPost, d.BaseURL()+"/api/v1/auth-sessions", strings.NewReader(`{"provider_spec":"chatgpt","draft_subject":"draft","auth_mode":"browser"}`))
	if err != nil {
		t.Fatalf("build child auth start request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "process-death-browser-auth-key")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("send child auth start request: %v", err)
	}
	_, readErr := io.Copy(io.Discard, response.Body)
	closeErr := response.Body.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		t.Fatalf("read child auth start response: %v", err)
	}
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("child auth start status=%d", response.StatusCode)
	}
	if err := os.WriteFile(readyPath, []byte("browser auth handoff retained\n"), 0600); err != nil {
		t.Fatalf("publish child auth handoff readiness: %v", err)
	}
	_, _ = os.Stdin.Read(make([]byte, 1))
}

func TestDaemonAuthStartProcessDeathForbidsRedispatch(t *testing.T) {
	t.Setenv("DO_NOT_TRACK", "1")
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(directory, "swobu.yaml")
	if err := os.WriteFile(configPath, []byte("schema_version: 5\nworkspaces: {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestDaemonAuthStartCrashHelper$", "-test.count=1")
	readyPath := filepath.Join(directory, "handoff-ready")
	command.Env = append(os.Environ(), "SWOBU_DAEMON_AUTH_START_CRASH_CONFIG="+configPath, "SWOBU_DAEMON_AUTH_START_CRASH_READY="+readyPath, "DO_NOT_TRACK=1")
	childStderr, err := os.Create(filepath.Join(directory, "child-stderr.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer childStderr.Close()
	command.Stderr = childStderr
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if command.ProcessState == nil {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	})
	deadline := time.NewTimer(20 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		ready, err := os.ReadFile(readyPath)
		if err == nil && string(ready) == "browser auth handoff retained\n" {
			break
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("inspect child auth readiness: %v", err)
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			current, _ := os.ReadFile(readyPath)
			_ = command.Process.Kill()
			_ = command.Wait()
			if _, err := childStderr.Seek(0, 0); err != nil {
				t.Fatal(err)
			}
			stderr, err := io.ReadAll(childStderr)
			if err != nil {
				t.Fatal(err)
			}
			t.Fatalf("child daemon browser auth handoff timed out at %q: %s", current, stderr)
		}
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err == nil {
		t.Fatal("child daemon was not killed")
	}
	entriesBeforeRestart, err := os.ReadDir(filepath.Join(directory, "auth-start-attempts"))
	if err != nil || len(entriesBeforeRestart) != 1 {
		t.Fatalf("crashed daemon retained attempts=%v error=%v", entriesBeforeRestart, err)
	}

	restarted, err := Start(context.Background(), StartInput{
		ConfigPath:    configPath,
		StartupConfig: platformconfig.StartupConfig{Addr: "127.0.0.1:0"},
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("restart daemon after process death: %v", err)
	}
	defer restarted.Close()
	retry, err := http.NewRequest(http.MethodPost, restarted.BaseURL()+"/api/v1/auth-sessions", strings.NewReader(`{"provider_spec":"chatgpt","draft_subject":"draft","auth_mode":"browser"}`))
	if err != nil {
		t.Fatalf("build restart auth request: %v", err)
	}
	retry.Header.Set("Content-Type", "application/json")
	retry.Header.Set("Idempotency-Key", "process-death-browser-auth-key")
	response, err := http.DefaultClient.Do(retry)
	if err != nil {
		t.Fatalf("send restart auth request: %v", err)
	}
	body, readErr := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		t.Fatalf("read restart auth response: %v", err)
	}
	if response.StatusCode != http.StatusConflict || !strings.Contains(string(body), "AUTH_START_INDETERMINATE") {
		t.Fatalf("restart auth status=%d body=%s", response.StatusCode, body)
	}
	entries, err := os.ReadDir(filepath.Join(directory, "auth-start-attempts"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("retained attempts=%v error=%v", entries, err)
	}
}
