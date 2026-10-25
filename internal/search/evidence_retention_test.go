package search

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSearchEvidenceRetentionReopensBoundedHeadAndFlushesOnClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "search.json")
	store, err := OpenEvidenceStore(path, 2)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"one", "two", "three"} {
		store.Append(Evidence{Operation: id, Outcome: "succeeded"})
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenEvidenceStore(path, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	events := reopened.Events()
	if len(events) != 2 || events[0].Operation != "two" || events[1].Operation != "three" {
		t.Fatalf("retained bounded head: %+v", events)
	}
	events[0].Operation = "mutated"
	if reopened.Events()[0].Operation != "two" {
		t.Fatal("retained read leaked mutable slice")
	}
}

func TestSearchEvidenceCorruptSnapshotIsExplicitAndNeverOverwritten(t *testing.T) {
	for _, raw := range []string{`not JSON`, `{"schema_version":2,"events":[]}`, `{"schema_version":1,"events":[]} {}`, `{"schema_version":1,"events":[],"unknown":true}`} {
		t.Run(raw, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "search.json")
			if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
				t.Fatal(err)
			}
			store, err := OpenEvidenceStore(path, 2)
			if !errors.Is(err, ErrEvidenceRetention) || !store.RetentionError() {
				t.Fatal("corrupt history looked empty/healthy")
			}
			store.Append(Evidence{Operation: "new", Outcome: "started"})
			if !errors.Is(store.Close(), ErrEvidenceRetention) || len(store.Events()) != 1 {
				t.Fatal("live evidence unavailable after retained read failure")
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != raw {
				t.Fatal("corrupt history was repaired/overwritten")
			}
		})
	}
}

func TestSearchEvidenceWriteFailureDoesNotChangeSearchOrEnableFallback(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "history")
	if err := os.Mkdir(parent, 0700); err != nil {
		t.Fatal(err)
	}
	store, err := OpenEvidenceStore(filepath.Join(parent, "search.json"), 4)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(parent); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(parent, []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	svc := Service{ConnectionIDs: func(string) ([]string, bool) { return []string{"primary", "backup"}, true }, Sources: map[string]Source{"primary": StaticSource{{Title: "query"}}, "backup": StaticSource{{Title: "wrong"}}}, Evidence: store.Append}
	results, err := svc.Search(t.Context(), "research", Query{Text: "query"})
	if err != nil || !reflect.DeepEqual(results, []Result{{Title: "query"}}) {
		t.Fatalf("evidence changed request behavior: %+v %v", results, err)
	}
	if !errors.Is(store.Close(), ErrEvidenceRetention) || !store.RetentionError() || len(store.Events()) != 2 {
		t.Fatal("snapshot write failure hidden or live facts lost")
	}
}

func TestUnusedSearchEvidenceCreatesNoSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "search.json")
	store, err := OpenEvidenceStore(path, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unused Search created a history artifact: %v", err)
	}
}
