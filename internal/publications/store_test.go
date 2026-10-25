package publications

import (
	"bytes"
	"errors"
	"github.com/swobuforge/swobu/internal/routing"
	"testing"
)

func slug(t *testing.T, raw string) routing.WorkspaceSlug {
	t.Helper()
	v, err := routing.ParseWorkspaceSlug(raw)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func TestAtomicPublicationAndNoAmbientAuthority(t *testing.T) {
	var disk []byte
	write := func(raw []byte) error { disk = bytes.Clone(raw); return nil }
	s, err := OpenInline(nil, write)
	if err != nil {
		t.Fatal(err)
	}
	ws := slug(t, "engineering")
	if s.Has(ws) || s.HasPublicWorkspace() {
		t.Fatal("empty set grants reachability")
	}
	rev := s.Revision()
	if err = s.Set(ws, true, rev, func() error { return errors.New("denied") }); err == nil {
		t.Fatal("authorization ignored")
	}
	if s.Has(ws) {
		t.Fatal("denied publication committed")
	}
	if err = s.Set(ws, true, rev, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if !s.Has(ws) || len(disk) == 0 {
		t.Fatal("not published")
	}
	if err = s.Set(ws, false, rev, func() error { return nil }); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale revision allowed: %v", err)
	}
	rev = s.Revision()
	if err = s.Set(ws, false, rev, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if s.HasPublicWorkspace() {
		t.Fatal("unpublished Workspace still public")
	}
}
func TestPublicationRequiresCurrentOnlyAndCanonicalSet(t *testing.T) {
	for _, bad := range []string{`{"workspaces":null}`, `{"workspaces":["z","a"]}`, `{"workspaces":["a","a"]}`, `{"exposures":[]}`, `{"workspaces":["bad name"]}`} {
		if _, err := ValidateDocument([]byte(bad)); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
}
func TestFailedPublishDoesNotChangeRuntime(t *testing.T) {
	s, err := OpenInline(nil, func([]byte) error { return errors.New("disk full") })
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Set(slug(t, "tools"), true, s.Revision(), func() error { return nil }); err == nil {
		t.Fatal("accepted failed disk write")
	}
	if s.HasPublicWorkspace() {
		t.Fatal("phantom publication")
	}
}

func TestEmptyPublicationRevisionSurvivesRestart(t *testing.T) {
	var disk []byte
	s, err := OpenInline(nil, func(raw []byte) error { disk = bytes.Clone(raw); return nil })
	if err != nil {
		t.Fatal(err)
	}
	ws := slug(t, "engineering")
	for _, enabled := range []bool{true, false} {
		if err := s.Set(ws, enabled, s.Revision(), func() error { return nil }); err != nil {
			t.Fatal(err)
		}
	}
	reopened, err := OpenInline(disk, func([]byte) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Revision() != s.Revision() {
		t.Fatal("restart changed revision of identical publication membership")
	}
}
