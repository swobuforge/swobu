package accessstate

import (
	"errors"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/routing"
)

func TestServiceAuthorityCanRecheckAtOwnRevocationBoundary(t *testing.T) {
	store, err := OpenInline(nil, func([]byte) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	workspace := testWorkspace(t, "administration")
	principal, issued, err := store.Create("Admin", workspace)
	if err != nil {
		t.Fatal(err)
	}
	resolution, err := store.Authenticate(issued.Token)
	if err != nil {
		t.Fatal(err)
	}
	completed := make(chan error, 1)
	go func() {
		_, err := store.RevokePrincipalChecked(principal.ID, PrincipalRevision(principal), func() error {
			_, err := store.ResolveCredentialForService(resolution.CredentialID, routing.ServiceMCP)
			return err
		})
		completed <- err
	}()
	select {
	case err := <-completed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("effect-time admission deadlocked on its own authority owner")
	}
	if _, err := store.ResolveCredentialForService(resolution.CredentialID, routing.ServiceMCP); err == nil {
		t.Fatal("self-revocation retained authority")
	}
}

func TestFailedRevocationKeepsCommittedCredentialAuthority(t *testing.T) {
	fail := false
	store, err := OpenInline(nil, func([]byte) error {
		if fail {
			return errors.New("disk write failed")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	workspace := testWorkspace(t, "administration")
	principal, issued, err := store.Create("Admin", workspace)
	if err != nil {
		t.Fatal(err)
	}
	resolution, err := store.Authenticate(issued.Token)
	if err != nil {
		t.Fatal(err)
	}
	fail = true
	if _, err := store.RevokePrincipalChecked(principal.ID, PrincipalRevision(principal), func() error { return nil }); err == nil {
		t.Fatal("failed publication reported success")
	}
	if _, err := store.ResolveCredentialForService(resolution.CredentialID, routing.ServiceMCP); err != nil {
		t.Fatal("failed publication withdrew committed authority")
	}
}
