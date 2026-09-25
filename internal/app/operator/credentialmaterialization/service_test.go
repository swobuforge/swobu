package credentialmaterialization

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRetireOnlyReclaimsRegisteredBeforeToAfterRemoval(t *testing.T) {
	var reclaimed []string
	service, err := NewService(func(_, id, _ string) (string, error) { return "secret:" + id, nil }, func(_, ref string) error {
		reclaimed = append(reclaimed, ref)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	committed, err := service.Materialize(context.Background(), "openai", "committed")
	if err != nil {
		t.Fatal(err)
	}
	pending, err := service.Materialize(context.Background(), "openai", "pending")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Reconcile(map[string]struct{}{committed: {}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := service.Retire(map[string]string{committed: "openai"}, map[string]string{}); err != nil {
		t.Fatal(err)
	}
	if len(reclaimed) != 1 || reclaimed[0] != committed {
		t.Fatalf("reclaimed=%v, want only %q", reclaimed, committed)
	}
	if _, ok := service.pending[pending]; !ok {
		t.Fatalf("unrelated pending material %q was swept", pending)
	}
}

func TestReconcilePersistsRetiredFailureContinuesAndRetries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credential-material.json")
	failRef := ""
	var reclaimed []string
	service, err := NewService(func(_, id, _ string) (string, error) { return "secretfile:" + id, nil }, func(_, ref string) error {
		reclaimed = append(reclaimed, ref)
		if ref == failRef {
			return errors.New("store offline")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SetPendingPath(path); err != nil {
		t.Fatal(err)
	}
	failRef, err = service.Materialize(context.Background(), "openai", "one")
	if err != nil {
		t.Fatal(err)
	}
	okRef, err := service.Materialize(context.Background(), "openai", "two")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Reconcile(map[string]struct{}{}, time.Now().Add(48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if service.pending[failRef].State != stateRetired {
		t.Fatalf("failed reclaim state=%q", service.pending[failRef].State)
	}
	if _, ok := service.pending[okRef]; ok {
		t.Fatalf("successful reclaim %q remained registered", okRef)
	}

	restarted, err := NewService(service.store, func(_, ref string) error { reclaimed = append(reclaimed, ref); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.SetPendingPath(path); err != nil {
		t.Fatal(err)
	}
	if err := restarted.Reconcile(map[string]struct{}{}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if len(restarted.pending) != 0 {
		t.Fatalf("retired retry remained registered: %+v", restarted.pending)
	}
}

func TestRegistryValidationFailsClosed(t *testing.T) {
	tests := map[string]string{
		"malformed":         `{`,
		"version":           `{"schema_version":2,"records":[]}`,
		"duplicate":         `{"schema_version":1,"records":[{"provider":"openai","ref":"secret:cred_a","state":"committed","expires_at":"0001-01-01T00:00:00Z"},{"provider":"openai","ref":"secret:cred_a","state":"committed","expires_at":"0001-01-01T00:00:00Z"}]}`,
		"unknown state":     `{"schema_version":1,"records":[{"provider":"openai","ref":"secret:cred_a","state":"mystery","expires_at":"0001-01-01T00:00:00Z"}]}`,
		"impossible expiry": `{"schema_version":1,"records":[{"provider":"openai","ref":"secret:cred_a","state":"committed","expires_at":"2026-01-01T00:00:00Z"}]}`,
		"unknown field":     `{"schema_version":1,"records":[],"extra":true}`,
	}
	for name, document := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "credential-material.json")
			if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
				t.Fatal(err)
			}
			service, err := NewService(func(_, id, _ string) (string, error) { return "secret:" + id, nil })
			if err != nil {
				t.Fatal(err)
			}
			if err := service.SetPendingPath(path); err == nil {
				t.Fatal("invalid registry was accepted")
			}
		})
	}
}

func TestMaterializeAllocatesFreshImmutableStorageIdentity(t *testing.T) {
	stored := map[string]string{}
	service, err := NewService(func(_ string, id string, secret string) (string, error) {
		stored[id] = secret
		return "secretfile:" + id, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	ref1, err := service.Materialize(context.Background(), "openai", "secret-one")
	if err != nil {
		t.Fatal(err)
	}
	ref2, err := service.Materialize(context.Background(), "openai", "secret-two")
	if err != nil {
		t.Fatal(err)
	}
	if ref1 == ref2 {
		t.Fatalf("materializations reused ref %q", ref1)
	}
	if stored[ref1[len("secretfile:"):]] != "secret-one" || stored[ref2[len("secretfile:"):]] != "secret-two" {
		t.Fatalf("materialized values were not independently retained")
	}
}

func TestPendingMaterialSurvivesServiceRestartAndReconciles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pending.json")
	var reclaimed []string
	store := func(_ string, id string, _ string) (string, error) { return "secretfile:" + id, nil }
	first, err := NewService(store, func(provider, ref string) error { reclaimed = append(reclaimed, provider+":"+ref); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := first.SetPendingPath(path); err != nil {
		t.Fatal(err)
	}
	ref, err := first.Materialize(context.Background(), "openai", "secret")
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewService(store, func(provider, ref string) error { reclaimed = append(reclaimed, provider+":"+ref); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := second.SetPendingPath(path); err != nil {
		t.Fatal(err)
	}
	if err := second.Reconcile(map[string]struct{}{}, time.Now().Add(48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if len(reclaimed) != 1 || reclaimed[0] != "openai:"+ref {
		t.Fatalf("reclaimed = %v", reclaimed)
	}
	if _, err := os.Stat(path); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestCommittedPendingMaterialIsRetainedAfterRestartReconciliation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pending.json")
	store := func(_ string, id string, _ string) (string, error) { return "secretfile:" + id, nil }
	first, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.SetPendingPath(path); err != nil {
		t.Fatal(err)
	}
	ref, err := first.Materialize(context.Background(), "openai", "secret")
	if err != nil {
		t.Fatal(err)
	}
	var reclaimed []string
	second, err := NewService(store, func(_, ref string) error { reclaimed = append(reclaimed, ref); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := second.SetPendingPath(path); err != nil {
		t.Fatal(err)
	}
	if err := second.Reconcile(map[string]struct{}{ref: {}}, time.Now().Add(48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if len(reclaimed) != 0 {
		t.Fatalf("committed ref reclaimed: %v", reclaimed)
	}
}

func TestRetireNeverReclaimsUnregisteredCredPrefix(t *testing.T) {
	called := false
	service, err := NewService(func(_, id, _ string) (string, error) { return "secret:" + id, nil }, func(_, _ string) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Retire(map[string]string{"secret:cred_user_owned": "openai"}, map[string]string{}); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("unregistered cred_ reference was reclaimed")
	}
}

func TestMaterializeReconcileReclaimsExpiredUncommittedMaterialButKeepsReferenced(t *testing.T) {
	var reclaimed []string
	service, err := NewService(func(_, id, _ string) (string, error) { return "secretfile:" + id, nil }, func(provider, id string) error {
		reclaimed = append(reclaimed, provider+":"+id)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := service.Materialize(context.Background(), "openai", "secret")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Reconcile(map[string]struct{}{ref: {}}, time.Now().Add(48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if len(reclaimed) != 0 {
		t.Fatalf("promoted material reclaimed: %v", reclaimed)
	}
	if _, err := service.Materialize(context.Background(), "openai", "abandoned"); err != nil {
		t.Fatal(err)
	}
	if err := service.Reconcile(map[string]struct{}{}, time.Now().Add(48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if len(reclaimed) != 2 {
		t.Fatalf("reclaimed=%v, want one abandoned material", reclaimed)
	}
}
