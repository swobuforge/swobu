package connect

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/clientconnect"
)

func TestConnectAttemptCrashHelper(t *testing.T) {
	directory := os.Getenv("SWOBU_CONNECT_CRASH_DIRECTORY")
	if directory == "" {
		return
	}
	service, err := NewService(clientconnect.NewService(), testCredentials{})
	if err != nil {
		t.Fatal(err)
	}
	target, err := clientconnect.NewTarget("research", "http://127.0.0.1:7926/workspaces/research")
	if err != nil {
		t.Fatal(err)
	}
	target = target.WithModelID("coding").WithAuthentication("structural-test-principal", "")
	preview, err := service.Plan(t.Context(), "codex", target)
	if err != nil {
		t.Fatal(err)
	}
	review := preview.Review()
	attempts, err := OpenAttempts(directory)
	if err != nil {
		t.Fatal(err)
	}
	stage := os.Getenv("SWOBU_CONNECT_CRASH_STAGE")
	if stage == "completed" {
		if _, err := service.ApplyAttempt(t.Context(), attempts, "process-death-original-key", "codex", review, target, func() error { return nil }); err != nil {
			t.Fatal(err)
		}
	} else {
		if err := attempts.Begin("process-death-original-key", "codex", review, target); err != nil {
			t.Fatal(err)
		}
	}
	if stage == "committed" || stage == "partial-completion" {
		if _, err := service.Apply(t.Context(), "codex", review, target); err != nil {
			t.Fatal(err)
		}
		if stage == "partial-completion" {
			path, _, err := attempts.identity("process-death-original-key", "codex", review, target)
			if err != nil {
				t.Fatal(err)
			}
			file, err := os.OpenFile(path+".completed", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			if _, err := file.Write([]byte(`{"intent":`)); err != nil {
				t.Fatal(err)
			}
			if err := file.Sync(); err != nil {
				t.Fatal(err)
			}
		}
	}
	fmt.Fprintln(os.Stdout, "CONNECT_ATTEMPT_BOUNDARY_READY")
	_, _ = os.Stdin.Read(make([]byte, 1))
}

func TestConnectAttemptProcessDeathRetainsNoRedispatchBoundary(t *testing.T) {
	for _, stage := range []string{"admitted", "committed", "partial-completion", "completed"} {
		t.Run(stage, func(t *testing.T) {
			root := t.TempDir()
			clientDirectory := filepath.Join(root, "client")
			attemptDirectory := filepath.Join(root, "attempts")
			for _, directory := range []string{clientDirectory, attemptDirectory} {
				if err := os.Mkdir(directory, 0700); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("CODEX_HOME", clientDirectory)
			path := filepath.Join(clientDirectory, "config.toml")
			const original = "model = \"original\"\nunrelated = \"preserve\"\n[model_providers.swobu.http_headers]\nAuthorization=\"Bearer fixture-client-token\"\n"
			if err := os.WriteFile(path, []byte(original), 0600); err != nil {
				t.Fatal(err)
			}
			service, err := NewService(clientconnect.NewService(), testCredentials{})
			if err != nil {
				t.Fatal(err)
			}
			target, err := clientconnect.NewTarget("research", "http://127.0.0.1:7926/workspaces/research")
			if err != nil {
				t.Fatal(err)
			}
			target = target.WithModelID("coding").WithAuthentication("structural-test-principal", "")
			preview, err := service.Plan(t.Context(), "codex", target)
			if err != nil {
				t.Fatal(err)
			}
			review := preview.Review()
			command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestConnectAttemptCrashHelper$", "-test.count=1")
			command.Env = append(os.Environ(), "SWOBU_CONNECT_CRASH_DIRECTORY="+attemptDirectory, "SWOBU_CONNECT_CRASH_STAGE="+stage)
			stdout, err := command.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
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
			ready := make(chan bool, 1)
			go func() {
				scanner := bufio.NewScanner(stdout)
				for scanner.Scan() {
					if scanner.Text() == "CONNECT_ATTEMPT_BOUNDARY_READY" {
						ready <- true
						return
					}
				}
				ready <- false
			}()
			select {
			case observed := <-ready:
				if !observed {
					t.Fatal("child exited before selected attempt boundary")
				}
			case <-time.After(10 * time.Second):
				t.Fatal("child did not reach selected attempt boundary")
			}
			if err := command.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			if err := command.Wait(); err == nil {
				t.Fatal("child did not die abruptly")
			}
			attempts, err := OpenAttempts(attemptDirectory)
			if err != nil {
				t.Fatal(err)
			}
			markers, err := os.ReadDir(attemptDirectory)
			count := 2
			if stage == "partial-completion" || stage == "completed" {
				count = 3
			}
			if err != nil || len(markers) != count {
				t.Fatal("process death lost exact attempt attribution")
			}
			if !errors.Is(attempts.Begin("process-death-original-key", "codex", review, target), ErrAttemptIndeterminate) {
				t.Fatal("process death allowed original attempt to redispatch")
			}
			observation, err := service.Reconcile(t.Context(), "codex", review, target)
			want := "unchanged"
			if stage != "admitted" {
				want = "configured"
			}
			if err != nil || observation.State != want {
				t.Fatal("post-death observation lost effect uncertainty")
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if stage == "admitted" && string(raw) != original {
				t.Fatal("pre-effect process death changed client file")
			}
			if stage != "admitted" {
				backup, err := os.ReadFile(path + ".swobu.bak")
				if err != nil || string(backup) != original {
					t.Fatal("post-commit recovery lost original backup")
				}
			}
			result, applyErr := service.ApplyAttempt(t.Context(), attempts, "process-death-original-key", "codex", review, target, func() error { return nil })
			if stage == "completed" {
				if applyErr != nil || !result.Replayed || !result.AlreadyConfigured {
					t.Fatal("durable completion did not recover explicit replay")
				}
			} else if !errors.Is(applyErr, ErrAttemptIndeterminate) || result.Replayed {
				t.Fatal("incomplete attempt fabricated replay or redispatched")
			}
			afterRecovery, err := os.ReadFile(path)
			if err != nil || string(afterRecovery) != string(raw) {
				t.Fatal("post-death retry changed client file")
			}
		})
	}
}
