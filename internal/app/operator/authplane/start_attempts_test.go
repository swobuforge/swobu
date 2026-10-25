package authplane

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func privateStartAttempts(t *testing.T) (*StartAttempts, string) {
	t.Helper()
	directory := filepath.Join(t.TempDir(), "starts")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	attempts, err := OpenStartAttempts(directory)
	if err != nil {
		t.Fatal(err)
	}
	return attempts, directory
}

func TestStartAttemptLostAcknowledgementAndReopenNeverRedispatch(t *testing.T) {
	for _, driverFails := range []bool{false, true} {
		t.Run(map[bool]string{false: "acknowledged", true: "uncertain"}[driverFails], func(t *testing.T) {
			attempts, directory := privateStartAttempts(t)
			starts := 0
			driver := driverStub{startFn: func(context.Context, StartInput) (DriverStartResult, error) {
				starts++
				if driverFails {
					return DriverStartResult{}, errors.New("lost provider acknowledgement")
				}
				return DriverStartResult{SessionID: "original"}, nil
			}}
			manager, _ := NewAuthSessionManager(driver)
			input := StartInput{ProviderSpec: " ChatGPT ", DraftSubject: "private-target-locator", AuthMode: "device"}
			_, err := manager.StartAttempt(t.Context(), attempts, "original-start-key", input, func() error { return nil })
			if (err != nil) != driverFails {
				t.Fatalf("initial outcome: %v", err)
			}
			if driverFails && !errors.Is(err, ErrStartIndeterminate) {
				t.Fatalf("first failed dispatch lost uncertainty: %v", err)
			}
			reopened, err := OpenStartAttempts(directory)
			if err != nil {
				t.Fatal(err)
			}
			restarted, _ := NewAuthSessionManager(driver)
			_, err = restarted.StartAttempt(t.Context(), reopened, "original-start-key", input, func() error { return nil })
			if !errors.Is(err, ErrStartIndeterminate) || starts != 1 {
				t.Fatalf("reopen redispatch: starts=%d error=%v", starts, err)
			}
			input.AuthMode = "browser"
			_, err = restarted.StartAttempt(t.Context(), reopened, "original-start-key", input, func() error { return nil })
			if !errors.Is(err, ErrStartConflict) || starts != 1 {
				t.Fatalf("changed intent: %v starts=%d", err, starts)
			}
			entries, err := os.ReadDir(directory)
			if err != nil || len(entries) != 1 {
				t.Fatalf("entries=%v error=%v", entries, err)
			}
			raw, err := os.ReadFile(filepath.Join(directory, entries[0].Name()))
			if err != nil {
				t.Fatal(err)
			}
			for _, private := range []string{"private-target-locator", "original-start-key", "device", "chatgpt"} {
				if strings.Contains(string(raw), private) || strings.Contains(entries[0].Name(), private) {
					t.Fatal("marker disclosed private intent")
				}
			}
		})
	}
}

func TestStartAttemptConcurrentManagersDispatchOnce(t *testing.T) {
	_, directory := privateStartAttempts(t)
	var starts atomic.Int32
	var workers sync.WaitGroup
	for range 16 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			reopened, err := OpenStartAttempts(directory)
			if err != nil {
				t.Error(err)
				return
			}
			manager, _ := NewAuthSessionManager(driverStub{startFn: func(context.Context, StartInput) (DriverStartResult, error) {
				starts.Add(1)
				return DriverStartResult{SessionID: "original"}, nil
			}})
			_, err = manager.StartAttempt(t.Context(), reopened, "concurrent-start-key", StartInput{ProviderSpec: "chatgpt", DraftSubject: "draft", AuthMode: "browser"}, func() error { return nil })
			if err != nil && !errors.Is(err, ErrStartIndeterminate) {
				t.Error(err)
			}
		}()
	}
	workers.Wait()
	if starts.Load() != 1 {
		t.Fatalf("starts=%d", starts.Load())
	}
}

func TestStartAttemptAdmissionAndPublicationFailureForbidEffects(t *testing.T) {
	for _, failure := range []string{"denied", "withdrawn", "canceled", "invalid", "invalid-mode", "directory-gone", "partial-marker", "symlink-marker"} {
		t.Run(failure, func(t *testing.T) {
			attempts, directory := privateStartAttempts(t)
			starts, calls := 0, 0
			manager, _ := NewAuthSessionManager(driverStub{startFn: func(context.Context, StartInput) (DriverStartResult, error) {
				starts++
				return DriverStartResult{SessionID: "original"}, nil
			}})
			input := StartInput{ProviderSpec: "chatgpt", DraftSubject: "draft", AuthMode: "browser"}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if failure == "canceled" {
				cancel()
			}
			if failure == "invalid" {
				input.ProviderSpec = ""
			}
			if failure == "invalid-mode" {
				input.AuthMode = "future"
			}
			if failure == "directory-gone" {
				if err := os.Remove(directory); err != nil {
					t.Fatal(err)
				}
			}
			if failure == "partial-marker" || failure == "symlink-marker" {
				if err := attempts.begin("original-start-key", input); err != nil {
					t.Fatal(err)
				}
				entries, _ := os.ReadDir(directory)
				path := filepath.Join(directory, entries[0].Name())
				if failure == "partial-marker" {
					if err := os.WriteFile(path, []byte("partial"), 0600); err != nil {
						t.Fatal(err)
					}
				} else {
					if err := os.Remove(path); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(filepath.Join(directory, "absent"), path); err != nil {
						t.Skip(err)
					}
				}
			}
			_, err := manager.StartAttempt(ctx, attempts, "original-start-key", input, func() error {
				calls++
				if failure == "denied" || failure == "withdrawn" && calls == 2 {
					return errors.New("admission withdrawn")
				}
				return nil
			})
			if err == nil || starts != 0 {
				t.Fatalf("failure=%s starts=%d error=%v", failure, starts, err)
			}
		})
	}
}

func TestOpenStartAttemptsAdmitsOperatorSelectedDirectories(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "shared")
	if err := os.Mkdir(directory, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(directory, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenStartAttempts(directory); err != nil {
		t.Fatalf("directory rejected: %v", err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(directory, alias); err == nil {
		if _, err := OpenStartAttempts(alias); err != nil {
			t.Fatalf("directory alias rejected: %v", err)
		}
	}
}

func TestStartAttemptNormalizedIntentAndInvalidKeys(t *testing.T) {
	attempts, directory := privateStartAttempts(t)
	starts := 0
	manager, _ := NewAuthSessionManager(driverStub{startFn: func(_ context.Context, in StartInput) (DriverStartResult, error) {
		starts++
		if in.ProviderSpec != "chatgpt" || in.DraftSubject != "draft" || in.AuthMode != "browser" {
			t.Fatalf("driver intent differs from retained intent: %#v", in)
		}
		return DriverStartResult{SessionID: "original"}, nil
	}})
	input := StartInput{ProviderSpec: " ChatGPT ", DraftSubject: " draft ", AuthMode: " browser "}
	for _, key := range []string{"", "short", strings.Repeat("x", 257), "bad key with spaces", "invalid-key-line\n", "non-ascii-key-ééé"} {
		if _, err := manager.StartAttempt(t.Context(), attempts, key, input, func() error { return nil }); err == nil {
			t.Fatalf("key admitted: %q", key)
		}
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 0 || starts != 0 {
		t.Fatalf("invalid key reserved/dispatched: entries=%d starts=%d error=%v", len(entries), starts, err)
	}
	if _, err := manager.StartAttempt(t.Context(), attempts, "original-start-key", input, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	input = StartInput{ProviderSpec: "chatgpt", DraftSubject: "draft", AuthMode: "browser"}
	if _, err := manager.StartAttempt(t.Context(), attempts, "original-start-key", input, func() error { return nil }); !errors.Is(err, ErrStartIndeterminate) {
		t.Fatalf("normalized intent did not match: %v", err)
	}
	if starts != 1 {
		t.Fatalf("starts=%d", starts)
	}
}
