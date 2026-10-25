package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	operatorclient "github.com/swobuforge/swobu/internal/app/operator/client"
)

func TestConnectRecoveryCreateOnceAndReopen(t *testing.T) {
	path := filepath.Join(privateConnectDirectory(t), "intent.json")
	store, err := openConnectRecovery(path)
	if err != nil {
		t.Fatal(err)
	}
	intent := reviewedConnectFixture(t)
	if err := store.retain(intent); err != nil {
		t.Fatal(err)
	}
	if err := store.retain(intent); err != nil {
		t.Fatal(err)
	}
	store.root.Close()
	store, err = openConnectRecovery(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.root.Close()
	restored, err := store.load()
	if err != nil {
		t.Fatal(err)
	}
	original, _ := intent.RecoveryRecord()
	raw, _ := restored.RecoveryRecord()
	if !bytes.Equal(raw, original) {
		t.Fatal("reopen changed intent")
	}
	changedReview, _ := intent.Review()
	changedReview.Changes[0].After = "different"
	changed, err := operatorclient.NewClientConfigurationIntent(changedReview, "different-connect-key")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.retain(changed); err == nil {
		t.Fatal("different intent replaced original")
	}
	stored, _ := os.ReadFile(path)
	if !bytes.Equal(stored, original) {
		t.Fatal("replacement changed recovery")
	}
}

func TestConnectRecoveryConcurrentPublicationNeverReplaces(t *testing.T) {
	path := filepath.Join(privateConnectDirectory(t), "intent.json")
	var workers sync.WaitGroup
	for index := 0; index < 8; index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			store, err := openConnectRecovery(path)
			if err != nil {
				t.Error(err)
				return
			}
			defer store.root.Close()
			_ = store.retain(reviewedConnectFixture(t))
		}()
	}
	workers.Wait()
	store, err := openConnectRecovery(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.root.Close()
	if _, err := store.load(); err != nil {
		t.Fatal("publication was partial", err)
	}
}

func TestConnectRecoveryRejectsCorruptAndEscapingRecords(t *testing.T) {
	for _, mode := range []string{"symlink-file", "corrupt", "oversized"} {
		t.Run(mode, func(t *testing.T) {
			directory := privateConnectDirectory(t)
			path := filepath.Join(directory, "intent.json")
			intent := reviewedConnectFixture(t)
			raw, _ := intent.RecoveryRecord()
			if mode == "corrupt" {
				raw = []byte("broken-record")
			}
			if mode == "oversized" {
				raw = []byte(strings.Repeat("x", 1<<20+1))
			}
			if mode == "symlink-file" {
				original := filepath.Join(directory, "original")
				if err := os.WriteFile(original, raw, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(original, path); err != nil {
					t.Skip(err)
				}
			} else if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			store, err := openConnectRecovery(path)
			if err != nil {
				t.Fatal(err)
			}
			defer store.root.Close()
			if _, err := store.load(); err == nil {
				t.Fatal("invalid record loaded")
			}
			if err := store.retain(intent); err == nil {
				t.Fatal("invalid record replaced")
			}
		})
	}
}

func TestConnectRecoveryUsesTrustedOperatorSelectedPaths(t *testing.T) {
	directory := privateConnectDirectory(t)
	if err := os.Chmod(directory, 0755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(directory, alias); err != nil {
		t.Skip(err)
	}
	path := filepath.Join(alias, "intent.json")
	store, err := openConnectRecovery(path)
	if err != nil {
		t.Fatalf("trusted operator-selected alias rejected: %v", err)
	}
	defer store.root.Close()
	intent := reviewedConnectFixture(t)
	if err := store.retain(intent); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(directory, "intent.json"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.load(); err != nil {
		t.Fatalf("existing file permission incorrectly rejected: %v", err)
	}
}

func privateConnectDirectory(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	return directory
}
