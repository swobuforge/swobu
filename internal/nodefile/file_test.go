package nodefile

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"gopkg.in/yaml.v3"
)

func seed(t *testing.T) (*File, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "swobu.yaml")
	if err := os.WriteFile(path, []byte("schema_version: 5\nworkspaces: {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return f, path
}

func TestConnectionAndRoutingPublicationConvergeInOneYAML(t *testing.T) {
	f, path := seed(t)
	docs := []byte(`{"schema_version":1,"connections":[{"id":"cn_fixture","kind":"search","display_name":"Example","revision":"1","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z","spec":{"provider":"brave","endpoint":"https://example.test"}}]}`)
	if err := f.PublishConnections(docs); err != nil {
		t.Fatal(err)
	}
	if err := f.PublishRouting([]byte("schema_version: 5\nworkspaces:\n  engineering:\n    routes: []\n")); err != nil {
		t.Fatal(err)
	}
	got, err := f.ConnectionDocument()
	if err != nil {
		t.Fatal(err)
	}
	var current, expected any
	if err := json.Unmarshal(got, &current); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(docs, &expected); err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(current)
	b, _ := json.Marshal(expected)
	if !bytes.Equal(a, b) {
		t.Fatalf("Connection semantic data lost: %s vs %s", a, b)
	}
	raw, _ := os.ReadFile(path)
	if !bytes.Contains(raw, []byte("engineering:")) || !bytes.Contains(raw, []byte("connections:")) {
		t.Fatalf("lost independent sections: %s", raw)
	}
	if bytes.Contains(raw, []byte("\"connections\"")) {
		t.Fatal("nested JSON blob instead of YAML mapping")
	}
	if err := f.PublishConnections([]byte(`{"schema_version":1,"connections":[]}`)); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(path)
	if !bytes.Contains(raw, []byte("engineering:")) {
		t.Fatal("Connection edit rolled back latest Workspace")
	}
}

func TestConcurrentSectionWritersNeverLoseChanges(t *testing.T) {
	f, _ := seed(t)
	var wait sync.WaitGroup
	wait.Add(2)
	failures := make(chan error, 2)
	go func() {
		defer wait.Done()
		for i := 0; i < 40; i++ {
			if err := f.PublishRouting([]byte("schema_version: 5\nworkspaces:\n  engineering:\n    routes: []\n")); err != nil {
				failures <- err
				return
			}
		}
	}()
	go func() {
		defer wait.Done()
		for i := 0; i < 40; i++ {
			if err := f.PublishConnections([]byte(`{"schema_version":1,"connections":[]}`)); err != nil {
				failures <- err
				return
			}
		}
	}()
	wait.Wait()
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	raw, _, err := f.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte("engineering:")) || !bytes.Contains(raw, []byte("connections:")) {
		t.Fatalf("lost concurrent updates: %s", raw)
	}
}

func TestAtomicFailurePreservesState(t *testing.T) {
	f, path := seed(t)
	before, revision, err := f.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	expected := errors.New("disk unavailable")
	f.replace = func(string, []byte, os.FileMode) error { return expected }
	if err := f.PublishConnections([]byte(`{"schema_version":1,"connections":[]}`)); !errors.Is(err, expected) {
		t.Fatalf("write error=%v", err)
	}
	after, afterRevision, err := f.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) || revision != afterRevision {
		t.Fatal("failed write modified Node document")
	}
	// A newly opened reader must see the same bytes as the live instance.
	disk, _ := os.ReadFile(path)
	if !bytes.Equal(disk, before) {
		t.Fatal("modified disk on failed write")
	}
}

func TestMalformedNodeConfigFailsBeforeChanges(t *testing.T) {
	f, path := seed(t)
	cases := []string{
		"schema_version: 5\nconnections: [bad]\n",
		"schema_version: 5\nschema_version: 5\nworkspaces: {}\n",
		"schema_version: 5\nworkspaces: {}\n---\nworkspaces: {}\n",
	}
	for _, bad := range cases {
		if err := os.WriteFile(path, []byte(bad), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := f.ConnectionDocument(); err == nil {
			t.Fatalf("accepted malformed %s", bad)
		}
	}
}

func TestCompleteSnapshotDigest(t *testing.T) {
	f, _ := seed(t)
	first, revision, err := f.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	// This test documents the immutable-snapshot evidence used in the next
	// batch; whole-document mutation remains application-owned, not exposed
	// on this storage adapter while existing daemon snapshots are separate.
	if strings.TrimSpace(revision) == "" || len(first) == 0 {
		t.Fatal("missing Node revision")
	}
	var document yaml.Node
	if err := yaml.Unmarshal(first, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Content) != 1 {
		t.Fatal("not one document")
	}
}

func TestManagedCommitRejectsExternalModification(t *testing.T) {
	f, path := seed(t)
	if err := os.WriteFile(path, []byte("schema_version: 5\nworkspaces: {}\n# external edit\n"), 0600); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	if err := f.PublishConnections([]byte(`{"schema_version":1,"connections":[]}`)); err == nil {
		t.Fatal("overwrote unmanaged edit")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("external edit modified after rejected commit")
	}
	if err := f.PublishRouting([]byte("schema_version: 5\nworkspaces: {}\n")); err == nil {
		t.Fatal("routing edit overwrote unmanaged revision")
	}
}

func TestRejectsTrailingJSONAndPreservesNumericValues(t *testing.T) {
	f, _ := seed(t)
	if err := f.PublishConnections([]byte(`{"schema_version":1,"connections":[]} {"overlap":true}`)); err == nil {
		t.Fatal("accepted multiple JSON documents")
	}
	if err := f.PublishConnections([]byte(`{"schema_version":1,"connections":[],"sample":12.75}`)); err != nil {
		t.Fatal(err)
	}
	got, err := f.ConnectionDocument()
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Sample float64 `json:"sample"`
	}
	if err := json.Unmarshal(got, &document); err != nil || document.Sample != 12.75 {
		t.Fatalf("numeric round trip: %s (%v)", got, err)
	}
}
