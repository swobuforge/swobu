package publications

import (
	"errors"
	"testing"
	"time"
)

func TestPublicationAdmissionCanReadCurrentMembershipInsideMutation(t *testing.T) {
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
	workspace := slug(t, "administration")
	if err := store.Set(workspace, true, store.Revision(), func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	revision := store.Revision()
	completed := make(chan error, 1)
	go func() {
		completed <- store.Set(workspace, false, revision, func() error {
			if !store.Has(workspace) {
				return errors.New("current publication lost before admitted mutation")
			}
			return nil
		})
	}()
	select {
	case err := <-completed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("publication admission recursively acquired the owner lock")
	}
	if store.Has(workspace) {
		t.Fatal("self-unpublish left membership active")
	}
	fail = true
	if err := store.Set(workspace, true, store.Revision(), func() error { return nil }); err == nil {
		t.Fatal("failed publication succeeded")
	}
	if store.Has(workspace) {
		t.Fatal("failed publication replaced committed membership")
	}
}
