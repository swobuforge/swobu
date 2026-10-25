package connect

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
)

func TestAttemptCapacityPreservesExistingRecoveryAcrossOwners(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	owner, err := OpenAttempts(directory)
	if err != nil {
		t.Fatal(err)
	}
	owner.capacity = 3
	target, plan := testPlan(t)
	preview := projectPlan(target, plan)
	review := preview.Review()
	var workers sync.WaitGroup
	results := make(chan error, 16)
	for index := 0; index < cap(results); index++ {
		workers.Add(1)
		go func(key string) {
			defer workers.Done()
			other, err := OpenAttempts(directory)
			if err != nil {
				results <- err
				return
			}
			other.capacity = owner.capacity
			results <- other.Begin(key, "codex", review, target)
		}(fmt.Sprintf("capacity-test-key-%02d", index))
	}
	workers.Wait()
	close(results)
	admitted := 0
	for err := range results {
		if err == nil {
			admitted++
		} else if !errors.Is(err, ErrAttemptCapacity) {
			t.Fatal("capacity contention returned unexpected admission")
		}
	}
	if admitted != owner.capacity {
		t.Fatal("concurrent owners exceeded attempt capacity")
	}
	if !errors.Is(owner.Begin("new-over-capacity-key", "codex", review, target), ErrAttemptCapacity) {
		t.Fatal("capacity admitted another effect")
	}
	for index := 0; index < cap(results); index++ {
		key := fmt.Sprintf("capacity-test-key-%02d", index)
		path, identity, err := owner.identity(key, "codex", review, target)
		if err != nil {
			t.Fatal(err)
		}
		if exactAttempt(path, identity) && !errors.Is(owner.Begin(key, "codex", review, target), ErrAttemptIndeterminate) {
			t.Fatal("capacity lost existing attempt recovery")
		}
	}
}

func TestAttemptsRetainOriginalIntentAcrossReopenAndConcurrency(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	owner, err := OpenAttempts(directory)
	if err != nil {
		t.Fatal(err)
	}
	target, plan := testPlan(t)
	preview := projectPlan(target, plan)
	review := preview.Review()
	const key = "original-attempt-key"
	var workers sync.WaitGroup
	results := make(chan error, 16)
	for index := 0; index < cap(results); index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			results <- owner.Begin(key, "codex", review, target)
		}()
	}
	workers.Wait()
	close(results)
	admitted := 0
	for err := range results {
		if err == nil {
			admitted++
		} else if !errors.Is(err, ErrAttemptIndeterminate) {
			t.Fatal("concurrent attempt lost indeterminate attribution")
		}
	}
	if admitted != 1 {
		t.Fatal("attempt dispatched more than once")
	}
	reopened, err := OpenAttempts(directory)
	if err != nil {
		t.Fatal(err)
	}
	if !errors.Is(reopened.Begin(key, "codex", review, target), ErrAttemptIndeterminate) {
		t.Fatal("reopen allowed uncertain attempt to dispatch again")
	}
	changed := review
	changed.Changes = []Change{{Field: "backend", Before: "other", BeforeExists: true, After: "new"}}
	changed.Revision = reviewRevision(target, plan.ClientID, changed)
	if !errors.Is(reopened.Begin(key, "codex", changed, target), ErrAttemptConflict) {
		t.Fatal("changed intent reused original key")
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 2 {
		t.Fatal("unexpected durable attempt inventory")
	}
	path, _, err := reopened.identity(key, "codex", review, target)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(reopened.Begin(key, "codex", review, target), ErrAttemptIndeterminate) {
		t.Fatal("partial marker allowed redispatch")
	}
}

func TestAttemptsRejectInvalidReviewBeforeMarkerCreation(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	owner, err := OpenAttempts(directory)
	if err != nil {
		t.Fatal(err)
	}
	target, plan := testPlan(t)
	preview := projectPlan(target, plan)
	review := preview.Review()
	if owner.Begin("short", "codex", review, target) == nil || owner.Begin("original-attempt-key", "claude", review, target) == nil || owner.Begin("original-attempt-key", "codex", Review{}, target) == nil {
		t.Fatal("invalid original intent admitted")
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 0 {
		t.Fatal("rejected intent persisted marker")
	}
	if err := os.Chmod(directory, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenAttempts(directory); err != nil {
		t.Fatalf("trusted local directory rejected: %v", err)
	}
}

func TestApplyAttemptChecksAdmissionAndNeverRepeatsUncertainEffect(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	attempts, err := OpenAttempts(directory)
	if err != nil {
		t.Fatal(err)
	}
	target, plan := testPlan(t)
	clients := &fakeClients{plan: plan, applyError: errors.New("acknowledgement unavailable")}
	service, err := NewService(clients, testCredentials{})
	if err != nil {
		t.Fatal(err)
	}
	preview := projectPlan(target, plan)
	review := preview.Review()
	denied := errors.New("Node admission revoked")
	if _, err := service.ApplyAttempt(t.Context(), attempts, "original-attempt-key", "codex", review, target, func() error { return denied }); !errors.Is(err, denied) || clients.applied {
		t.Fatal("denied attempt reached effect")
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 0 {
		t.Fatal("denial retained attempt")
	}
	if _, err := service.ApplyAttempt(t.Context(), attempts, "original-attempt-key", "codex", review, target, func() error { return nil }); !errors.Is(err, clients.applyError) || !clients.applied {
		t.Fatal("first attempt lost effect uncertainty")
	}
	clients.applied = false
	if _, err := service.ApplyAttempt(t.Context(), attempts, "original-attempt-key", "codex", review, target, func() error { return nil }); !errors.Is(err, ErrAttemptIndeterminate) || clients.applied {
		t.Fatal("uncertain original attempt repeated effect")
	}
}
