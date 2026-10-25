package connections

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestStoreRestartStaleWriteAndAtomicFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "swobu.yaml")
	store, err := openInlineStoreFixture(t, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	original := fixtureConnection(t)
	if err := store.Publish(original, Revision{}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := original.EditSameIdentity(original.Revision(), "Changed", original.Spec(), original.UpdatedAt().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	replace := store.publishSection
	store.publishSection = func([]byte) error { return errors.New("injected write failure") }
	if err := store.Publish(updated, original.Revision()); err == nil {
		t.Fatal("accepted failed persistence")
	}
	current, _ := store.Get(original.ID())
	after, _ := os.ReadFile(path)
	if current.Revision() != original.Revision() || string(before) != string(after) {
		t.Fatal("failed write published state")
	}
	store.publishSection = replace
	if err := store.Publish(updated, original.Revision()); err != nil {
		t.Fatal(err)
	}
	if err := store.Publish(updated, original.Revision()); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale write=%v", err)
	}
	reopened, err := openInlineStoreFixture(t, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	current, err = reopened.Get(original.ID())
	if err != nil || current.DisplayName() != "Changed" || current.Revision().String() != "2" {
		t.Fatalf("restart=%v %v", current, err)
	}
	info, _ := os.Stat(path)
	// Windows mode bits do not describe inherited host DACLs. Persistence and
	// revision assertions above still run there; POSIX new-file modes stay exact.
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatal("registry is not private")
	}
}
func TestStoreConcurrentRevisionHasOneWinner(t *testing.T) {
	store, err := openInlineStoreFixture(t, filepath.Join(t.TempDir(), "swobu.yaml"), nil)
	if err != nil {
		t.Fatal(err)
	}
	original := fixtureConnection(t)
	if err := store.Publish(original, Revision{}); err != nil {
		t.Fatal(err)
	}
	candidate, _ := original.EditSameIdentity(original.Revision(), "Changed", original.Spec(), original.UpdatedAt().Add(time.Second))
	outcomes := make(chan error, 2)
	var group sync.WaitGroup
	for range 2 {
		group.Add(1)
		go func() { defer group.Done(); outcomes <- store.Publish(candidate, original.Revision()) }()
	}
	group.Wait()
	close(outcomes)
	wins, conflicts := 0, 0
	for err := range outcomes {
		if err == nil {
			wins++
		} else if errors.Is(err, ErrRevisionConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatalf("wins=%d conflicts=%d", wins, conflicts)
	}
}
func TestStoreRejectsCorruptDocuments(t *testing.T) {
	for _, raw := range []string{`null`, `{}`, `{"schema_version":2,"connections":[]}`, `{"schema_version":1,"connections":[],"secret":"private"}`, `{"schema_version":1,"connections":[]} {}`} {
		if _, err := OpenInline([]byte(raw), func([]byte) error { return nil }, nil); err == nil {
			t.Errorf("accepted corrupt document %s", raw)
		}
	}
}
