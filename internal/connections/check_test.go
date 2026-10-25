package connections

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestCheckObservationDoesNotPublishConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "swobu.yaml")
	store, err := openInlineStoreFixture(t, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	c := fixtureConnection(t)
	if err := store.Publish(c, Revision{}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	capabilities := []Capability{CapabilityTools}
	check, err := NewCheck(c, c.UpdatedAt().Add(time.Second), Reachable, AuthenticationAccepted, capabilities, CheckOK)
	if err != nil {
		t.Fatal(err)
	}
	capabilities[0] = CapabilityModels
	if err := store.Observe(check); err != nil {
		t.Fatal(err)
	}
	got, ok := store.LatestCheck(c.ID())
	if !ok || got.Capabilities()[0] != CapabilityTools {
		t.Fatal("missing or mutable observation")
	}
	exposed := got.Capabilities()
	exposed[0] = CapabilitySearch
	if got.Capabilities()[0] != CapabilityTools {
		t.Fatal("accessor exposed mutable capabilities")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(before) != string(after) {
		t.Fatal("observation rewrote configuration")
	}
	current, _ := store.Get(c.ID())
	if current.Revision() != c.Revision() {
		t.Fatal("check advanced configuration revision")
	}
	reopened, err := openInlineStoreFixture(t, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reopened.LatestCheck(c.ID()); ok {
		t.Fatal("restart invented retained health")
	}
	edited, err := c.EditSameIdentity(c.Revision(), "Edited", c.Spec(), c.UpdatedAt().Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Publish(edited, c.Revision()); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.LatestCheck(c.ID()); ok {
		t.Fatal("old health projected on edited configuration")
	}
	if err := store.Observe(check); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("late completion: %v", err)
	}
}

func TestConcurrentChecksKeepLatestObservation(t *testing.T) {
	store, err := openInlineStoreFixture(t, filepath.Join(t.TempDir(), "swobu.yaml"), nil)
	if err != nil {
		t.Fatal(err)
	}
	c := fixtureConnection(t)
	if err := store.Publish(c, Revision{}); err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	for i := 1; i <= 20; i++ {
		check, err := NewCheck(c, c.UpdatedAt().Add(time.Duration(i)*time.Second), Reachable, AuthenticationUnknown, nil, CheckOK)
		if err != nil {
			t.Fatal(err)
		}
		group.Add(1)
		go func() {
			defer group.Done()
			if err := store.Observe(check); err != nil {
				t.Error(err)
			}
		}()
	}
	group.Wait()
	got, ok := store.LatestCheck(c.ID())
	if !ok || !got.CheckedAt().Equal(c.UpdatedAt().Add(20*time.Second)) {
		t.Fatal("out-of-order completion replaced newer evidence")
	}
}

func TestCheckRejectsUnvalidatedAndUnsafeObservations(t *testing.T) {
	c := fixtureConnection(t)
	for _, test := range []struct {
		reach   Reachability
		auth    Authentication
		caps    []Capability
		failure CheckError
	}{
		{Reachability("ready"), AuthenticationUnknown, nil, CheckOK},
		{Reachable, Authentication("secret"), nil, CheckOK},
		{Unreachable, AuthenticationAccepted, nil, CheckOK},
		{Reachable, AuthenticationRejected, []Capability{CapabilityTools}, CheckAuthenticationFailed},
		{Unreachable, AuthenticationUnknown, []Capability{CapabilityTools}, CheckOK},
		{Reachable, AuthenticationUnknown, []Capability{CapabilityModels}, CheckOK},
		{Reachable, AuthenticationUnknown, []Capability{CapabilityTools, CapabilityTools}, CheckOK},
		{Reachable, AuthenticationUnknown, nil, CheckError("upstream secret response")},
	} {
		if _, err := NewCheck(c, c.UpdatedAt(), test.reach, test.auth, test.caps, test.failure); err == nil {
			t.Fatalf("accepted invalid observation: %+v", test)
		}
	}
	if _, err := NewCheck(Connection{}, c.UpdatedAt(), Reachable, AuthenticationUnknown, nil, CheckOK); err == nil {
		t.Fatal("accepted zero Connection")
	}
	if _, err := NewCheck(c, c.UpdatedAt().Add(-time.Second), Reachable, AuthenticationUnknown, nil, CheckOK); err == nil {
		t.Fatal("accepted check predating configuration")
	}
}
