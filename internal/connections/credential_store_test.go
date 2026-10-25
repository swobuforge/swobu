package connections

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/domain/credentialref"
)

func TestCredentialBindingPublicationFailureAndRestart(t *testing.T) {
	store, err := openInlineStoreFixture(t, filepath.Join(t.TempDir(), "swobu.yaml"), nil)
	if err != nil {
		t.Fatal(err)
	}
	c := fixtureConnection(t)
	if err := store.Publish(c, Revision{}); err != nil {
		t.Fatal(err)
	}
	old := credentialref.Parse("env:MCP_TOKEN")
	next := credentialref.Parse("env:NEXT")
	check, err := NewCheck(c, c.UpdatedAt(), Reachable, AuthenticationUnknown, nil, CheckOK)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Observe(check); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	replace := store.publishSection
	store.publishSection = func([]byte) error { return errors.New("injected write failure") }
	if _, err := store.ReplaceCredentialBinding(context.Background(), c.ID(), c.Revision(), "", old, next, c.UpdatedAt().Add(time.Second)); err == nil {
		t.Fatal("accepted failed publication")
	}
	after, err := os.ReadFile(store.path)
	if err != nil || string(before) != string(after) {
		t.Fatal("failed replacement changed file")
	}
	current, _ := store.Get(c.ID())
	if current.Revision() != c.Revision() || !reflect.DeepEqual(current.CredentialRefs(), []credentialref.Ref{old}) {
		t.Fatal("failed replacement changed memory")
	}
	if _, ok := store.LatestCheck(c.ID()); !ok {
		t.Fatal("failed replacement invalidated observation")
	}
	store.publishSection = replace
	committed, err := store.ReplaceCredentialBinding(context.Background(), c.ID(), c.Revision(), "", old, next, c.UpdatedAt().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if committed.ID() != c.ID() || committed.Revision().String() != "2" {
		t.Fatal("wrong committed identity")
	}
	if _, ok := store.LatestCheck(c.ID()); ok {
		t.Fatal("old health survived rotation")
	}
	reopened, err := openInlineStoreFixture(t, store.path, nil)
	if err != nil {
		t.Fatal(err)
	}
	current, err = reopened.Get(c.ID())
	if err != nil || !reflect.DeepEqual(current.CredentialRefs(), []credentialref.Ref{next}) || current.Revision() != committed.Revision() {
		t.Fatal("restart lost replacement")
	}
	if _, err := store.ReplaceCredentialBinding(context.Background(), c.ID(), c.Revision(), "", old, next, committed.UpdatedAt()); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale rotation: %v", err)
	}
}

func TestConcurrentCredentialBindingPublicationHasOneWinner(t *testing.T) {
	store, err := openInlineStoreFixture(t, filepath.Join(t.TempDir(), "swobu.yaml"), nil)
	if err != nil {
		t.Fatal(err)
	}
	c := fixtureConnection(t)
	if err := store.Publish(c, Revision{}); err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	results := make(chan error, 2)
	for _, raw := range []string{"env:FIRST", "env:SECOND"} {
		group.Add(1)
		go func() {
			defer group.Done()
			_, err := store.ReplaceCredentialBinding(context.Background(), c.ID(), c.Revision(), "", credentialref.Parse("env:MCP_TOKEN"), credentialref.Parse(raw), c.UpdatedAt().Add(time.Second))
			results <- err
		}()
	}
	group.Wait()
	close(results)
	wins, conflicts := 0, 0
	for err := range results {
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

func TestCredentialRefsAreDistinctSortedAndDetached(t *testing.T) {
	first := credentialref.Parse("env:ALPHA")
	last := credentialref.Parse("env:ZETA")
	spec, err := NewMCPStdioSpec("server", nil, map[string]credentialref.Ref{"A": last, "B": first, "C": last})
	if err != nil {
		t.Fatal(err)
	}
	base := fixtureConnection(t)
	c, err := New(base.ID(), "Stdio", spec, base.CreatedAt())
	if err != nil {
		t.Fatal(err)
	}
	refs := c.CredentialRefs()
	if !reflect.DeepEqual(refs, []credentialref.Ref{first, last}) {
		t.Fatal("references not distinct and ordered")
	}
	refs[0] = last
	if c.CredentialRefs()[0] != first {
		t.Fatal("mutable refs escaped")
	}
}
