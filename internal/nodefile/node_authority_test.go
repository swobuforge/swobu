package nodefile

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
)

const principalDoc = `{"schema_version":3,"access":[]}`
const publicationDoc = `{"workspaces":[]}`

func TestRoutingAndThreeAuthoritiesShareOneAtomicDocument(t *testing.T) {
	file, path := seed(t)
	if err := file.PublishPrincipals([]byte(principalDoc)); err != nil {
		t.Fatal(err)
	}
	if err := file.PublishPublications([]byte(publicationDoc)); err != nil {
		t.Fatal(err)
	}
	if err := file.PublishConnections([]byte(`{"schema_version":1,"connections":[]}`)); err != nil {
		t.Fatal(err)
	}
	if err := file.PublishRouting([]byte("schema_version: 5\nworkspaces:\n  engineering:\n    routes: []\n")); err != nil {
		t.Fatal(err)
	}
	for _, section := range []struct {
		get  func() ([]byte, error)
		want string
	}{
		{file.PrincipalDocument, `"access":[]`}, {file.PublicationDocument, `"workspaces":[]`}, {file.ConnectionDocument, `"connections":[]`},
	} {
		raw, err := section.get()
		if err != nil || !strings.Contains(string(raw), section.want) {
			t.Fatalf("lost section: %s %v", raw, err)
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, section := range []string{"workspaces:", "connections:", "principals:", "publications:"} {
		if !bytes.Contains(raw, []byte(section)) {
			t.Fatalf("missing %s", section)
		}
	}
	// Strict ownership: a routing writer cannot smuggle another section.
	if err := file.PublishRouting([]byte("schema_version: 5\nworkspaces: {}\nprincipals: {}\n")); err == nil {
		t.Fatal("routing replaced Principals")
	}
}

func TestFourConcurrentAuthorityWritersPreserveIndependentSections(t *testing.T) {
	file, _ := seed(t)
	var wg sync.WaitGroup
	errorsCh := make(chan error, 4)
	for _, job := range []func() error{
		func() error { return file.PublishRouting([]byte("schema_version: 5\nworkspaces: {}\n")) },
		func() error { return file.PublishConnections([]byte(`{"schema_version":1,"connections":[]}`)) },
		func() error { return file.PublishPrincipals([]byte(principalDoc)) },
		func() error { return file.PublishPublications([]byte(publicationDoc)) },
	} {
		wg.Add(1)
		go func(job func() error) {
			defer wg.Done()
			for range 20 {
				if err := job(); err != nil {
					errorsCh <- err
					return
				}
			}
		}(job)
	}
	wg.Wait()
	close(errorsCh)
	for err := range errorsCh {
		t.Fatal(err)
	}
	for _, get := range []func() ([]byte, error){file.ConnectionDocument, file.PrincipalDocument, file.PublicationDocument} {
		raw, err := get()
		if err != nil || len(raw) == 0 {
			t.Fatalf("lost concurrent section %s: %v", raw, err)
		}
	}
}

func TestPrincipalOrPublicationFailedReplaceKeepsBothAuthorities(t *testing.T) {
	file, path := seed(t)
	if err := file.PublishPrincipals([]byte(principalDoc)); err != nil {
		t.Fatal(err)
	}
	before, _, _ := file.Snapshot()
	injected := errors.New("unavailable")
	file.replace = func(string, []byte, os.FileMode) error { return injected }
	if err := file.PublishPublications([]byte(publicationDoc)); !errors.Is(err, injected) {
		t.Fatalf("err=%v", err)
	}
	after, _, _ := file.Snapshot()
	if !bytes.Equal(before, after) {
		t.Fatal("failed publication changed Node")
	}
	file2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	principals, err := file2.PrincipalDocument()
	if err != nil || len(principals) == 0 {
		t.Fatal("lost previous Principals", err)
	}
	publications, err := file2.PublicationDocument()
	if err != nil || len(publications) != 0 {
		t.Fatal("phantom publication", err)
	}
}

func TestManagedDocumentRejectsExternalMutationForPolicyWriters(t *testing.T) {
	file, path := seed(t)
	if err := os.WriteFile(path, []byte("schema_version: 5\nworkspaces: {}\n# external edit\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, update := range []func() error{
		func() error { return file.PublishPrincipals([]byte(principalDoc)) },
		func() error { return file.PublishPublications([]byte(publicationDoc)) },
	} {
		if err := update(); err == nil {
			t.Fatal("overwrote externally modified Node")
		}
	}
}
