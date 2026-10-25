package accessstate

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/nodefile"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestPrincipalGrantsAndCredentialDigestsAreInlineAndSurviveRestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "swobu.yaml")
	if err := os.WriteFile(path, []byte("schema_version: 5\nworkspaces: {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	node, err := nodefile.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := OpenInline(nil, node.PublishPrincipals)
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := routing.ParseWorkspaceSlug("research")
	if err != nil {
		t.Fatal(err)
	}
	principal, issued, err := authority.Create("Alice", workspace)
	if err != nil {
		t.Fatal(err)
	}
	if principal.Name != "Alice" {
		t.Fatal("missing Principal")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), issued.Token) || !strings.Contains(string(raw), "principals:") {
		t.Fatal("leaked bearer or lost inline authority")
	}
	if _, err := os.Stat(filepath.Join(dir, "access.json")); !os.IsNotExist(err) {
		t.Fatal("legacy sidecar written")
	}
	section, err := node.PrincipalDocument()
	if err != nil {
		t.Fatal(err)
	}
	reread, err := OpenInline(section, node.PublishPrincipals)
	if err != nil {
		t.Fatal(err)
	}
	resolution, err := reread.Authenticate(issued.Token)
	if err != nil || resolution.AccessID != principal.ID {
		t.Fatal("credential readback failed", err)
	}
	if err := reread.RevokePermission(principal.ID, workspace); err != nil {
		t.Fatal(err)
	}
	section, err = node.PrincipalDocument()
	if err != nil {
		t.Fatal(err)
	}
	reread, err = OpenInline(section, node.PublishPrincipals)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reread.Authenticate(issued.Token); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("revoked grant reauthorized: %v", err)
	}
}

func TestInlinePrincipalWriteFailureDoesNotMintAuthority(t *testing.T) {
	s, err := OpenInline(nil, func([]byte) error { return errors.New("disk failure") })
	if err != nil {
		t.Fatal(err)
	}
	ws, _ := routing.ParseWorkspaceSlug("research")
	if _, _, err := s.Create("Alice", ws); err == nil {
		t.Fatal("issued credential on failed write")
	}
	if len(s.List()) != 0 {
		t.Fatal("published in-memory access despite disk failure")
	}
}

func TestInlinePrincipalVersionFailsClosed(t *testing.T) {
	if _, err := ValidateDocument([]byte(`{"schema_version":1,"access":[]}`)); err == nil {
		t.Fatal("accepted old schema")
	}
	if _, err := ValidateDocument([]byte(`{"schema_version":2,"access":[]}`)); err == nil {
		t.Fatal(err)
	}
	if _, err := ValidateDocument([]byte(`{"schema_version":3,"access":[]}`)); err != nil {
		t.Fatal(err)
	}
}

func TestCredentialWithoutGrantCannotAllocateOrPublish(t *testing.T) {
	var document []byte
	publications := 0
	store, err := OpenInline(nil, func(raw []byte) error {
		publications++
		document = append([]byte(nil), raw...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	principal, _, err := store.CreatePrincipalChecked(t.Context(), "No grant", "no-grant-principal", func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	before := store.List()
	saved := append([]byte(nil), document...)
	count := publications
	// Exhausted entropy distinguishes admission before allocation from minting a
	// credential and only then noticing that its audience has no live grant.
	store.random = strings.NewReader("")
	workspace, _ := routing.ParseWorkspaceSlug("research")
	issued, err := store.IssueCredential(principal.ID, workspace)
	if !errors.Is(err, ErrUnauthorized) || issued.Token != "" {
		t.Fatal("ungranted issuance did not fail before credential allocation")
	}
	if publications != count || !bytes.Equal(saved, document) || !reflect.DeepEqual(before, store.List()) {
		t.Fatal("ungranted issuance changed authority or invoked publication")
	}
}
