package nodefile

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestExternallyManagedNodeCannotPublishAnySection(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "swobu.yaml")
	before := []byte("schema_version: 5\nworkspaces: {}\n")
	if err := os.WriteFile(path, before, 0444); err != nil {
		t.Fatal(err)
	}
	f, err := OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func() error{
		func() error { return f.PublishRouting(before) },
		func() error { return f.PublishConnections([]byte(`{"schema_version":1,"connections":[]}`)) },
		func() error { return f.PublishPrincipals([]byte(principalDoc)) },
		func() error { return f.PublishPublications([]byte(publicationDoc)) },
		func() error { return f.PublishAccounting([]byte(`{"schema_version":1,"limits":{}}`)) },
	} {
		if err := mutate(); !errors.Is(err, ErrReadOnly) {
			t.Fatalf("mutation=%v", err)
		}
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(before) {
		t.Fatal("modified externally managed YAML", err)
	}
	if _, err := os.Stat(path + ".lock"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("created a read-only lockfile")
	}
}
