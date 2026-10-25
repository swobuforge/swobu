package configstore

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestStartupRecoveryHoldsAndRetainsLifetimeLock(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "swobu.yaml")
	called := false
	store, err := OpenOrCreateAfterRecovery(path, func() error {
		called = true
		if second, err := OpenOrCreate(path); !errors.Is(err, ErrLocked) {
			if second != nil {
				second.Close()
			}
			t.Fatal("recovery ran outside lock", err)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("routing created before recovery")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if !called {
		t.Fatal("recovery omitted")
	}
	if second, err := Open(path); !errors.Is(err, ErrLocked) {
		if second != nil {
			second.Close()
		}
		t.Fatal("normal open lost lock", err)
	}
}

func TestStartupRecoveryFailureReleasesLockWithoutCreatingState(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "swobu.yaml")
	failure := errors.New("synthetic recovery failure")
	if store, err := OpenOrCreateAfterRecovery(path, func() error { return failure }); !errors.Is(err, failure) || store != nil {
		t.Fatal("recovery failure ignored", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("failed recovery created routing")
	}
	store, err := OpenOrCreate(path)
	if err != nil {
		t.Fatal("failed recovery leaked lock", err)
	}
	store.Close()
}
