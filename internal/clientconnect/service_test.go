package clientconnect

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestRunLocalCommandDoesNotWaitForDescendantOutputHandles(t *testing.T) {
	started := time.Now()
	stdout, exitCode, err := runLocalCommand(context.Background(), "sh", "-c", "(sleep 2) & printf ready")
	if err != nil {
		t.Fatalf("run local command: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0", exitCode)
	}
	if string(stdout) != "ready" {
		t.Fatalf("stdout = %q, want ready", stdout)
	}
	if elapsed := time.Since(started); elapsed >= time.Second {
		t.Fatalf("command waited %s for a descendant after the direct child exited", elapsed)
	}
}

func TestRunLocalCommandReturnsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, _, err := runLocalCommand(ctx, os.Args[0], "-test.run=^TestBlockingCommandHelper$", "--", "block")
		done <- err
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled command did not return promptly")
	}
}

func TestBlockingCommandHelper(t *testing.T) {
	if !slices.Contains(os.Args, "block") {
		return
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	if _, err := reader.Read(make([]byte, 1)); err != nil {
		t.Fatal(err)
	}
}

func TestApplyRequiresObservedConvergence(t *testing.T) {
	target := testTarget(t)
	state := "old"
	service, reviewed := reconciliationService(t, target, &state, nil)
	verified, err := service.Apply(context.Background(), reviewed)
	if err == nil || !strings.Contains(err.Error(), "did not converge") {
		t.Fatalf("error = %v", err)
	}
	if verified.AlreadyConfigured() || len(verified.Changes) == 0 {
		t.Fatalf("verified plan = %#v", verified)
	}
}

func TestApplyReturnsVerifiedSuccess(t *testing.T) {
	target := testTarget(t)
	state := "old"
	service, reviewed := reconciliationService(t, target, &state, func() error {
		state = target.WorkspaceURL()
		return nil
	})
	verified, err := service.Apply(context.Background(), reviewed)
	if err != nil || !verified.AlreadyConfigured() {
		t.Fatalf("verified = %#v, error = %v", verified, err)
	}
}

func TestApplyAcceptsAlreadyCompletedRaceWithoutMutation(t *testing.T) {
	target := testTarget(t)
	state := "old"
	applied := false
	service, reviewed := reconciliationService(t, target, &state, func() error {
		applied = true
		return nil
	})
	state = target.WorkspaceURL()
	verified, err := service.Apply(context.Background(), reviewed)
	if err != nil || !verified.AlreadyConfigured() || applied {
		t.Fatalf("verified = %#v, applied = %v, error = %v", verified, applied, err)
	}
}

func TestApplyReturnsCurrentPlanForStaleReview(t *testing.T) {
	target := testTarget(t)
	state := "old"
	applied := false
	service, reviewed := reconciliationService(t, target, &state, func() error {
		applied = true
		return nil
	})
	state = "newer"
	current, err := service.Apply(context.Background(), reviewed)
	if err == nil || !strings.Contains(err.Error(), "Open Connect again") || applied {
		t.Fatalf("current = %#v, applied = %v, error = %v", current, applied, err)
	}
	if len(current.Changes) != 1 || current.Changes[0].Before != "newer" {
		t.Fatalf("current plan = %#v", current)
	}
}

func TestApplyReturnsObservedPartialStateWithMutationError(t *testing.T) {
	target := testTarget(t)
	state := "old"
	mutationErr := errors.New("second write failed")
	service, reviewed := reconciliationService(t, target, &state, func() error {
		state = "partial"
		return mutationErr
	})
	current, err := service.Apply(context.Background(), reviewed)
	if !errors.Is(err, mutationErr) {
		t.Fatalf("error = %v", err)
	}
	if len(current.Changes) != 1 || current.Changes[0].Before != "partial" {
		t.Fatalf("current plan = %#v", current)
	}
}

func TestApplyDoesNotBeginMutationAfterCancellation(t *testing.T) {
	target := testTarget(t)
	state := "old"
	ctx, cancel := context.WithCancel(context.Background())
	applied := false
	inspections := 0
	original := adapters
	t.Cleanup(func() { adapters = original })
	const client ClientID = "reconciliation-test"
	adapters = []adapter{{
		id: client, name: "Reconciliation test", present: func(*Service) (bool, error) { return true, nil },
		planCurrent: func(context.Context, *Service, Target) (plannedMutation, error) {
			inspections++
			if inspections == 2 {
				cancel()
			}
			plan := Plan{ConfigPaths: []string{"/tmp/reconciliation"}, Target: target, Changes: semanticChange("endpoint", state, true, target.WorkspaceURL())}
			return plannedMutation{plan: plan, apply: func(context.Context) error {
				applied = true
				return nil
			}}, nil
		},
	}}
	service := &Service{}
	reviewed, err := service.Plan(context.Background(), client, target)
	if err != nil {
		t.Fatal(err)
	}
	current, err := service.Apply(ctx, reviewed)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context cancellation", err)
	}
	if applied {
		t.Fatal("mutation began after cancellation")
	}
	if len(current.Changes) != 1 || current.Changes[0].Before != state {
		t.Fatalf("current plan = %#v", current)
	}
}

func TestApplyPreservesMutationAndVerificationErrors(t *testing.T) {
	target := testTarget(t)
	mutationErr := errors.New("mutation failed")
	verificationErr := errors.New("verification failed")
	inspections := 0
	original := adapters
	t.Cleanup(func() { adapters = original })
	const client ClientID = "reconciliation-test"
	adapters = []adapter{{
		id: client, name: "Reconciliation test", present: func(*Service) (bool, error) { return true, nil },
		planCurrent: func(context.Context, *Service, Target) (plannedMutation, error) {
			inspections++
			if inspections == 3 {
				return plannedMutation{}, verificationErr
			}
			plan := Plan{ConfigPaths: []string{"/tmp/reconciliation"}, Target: target, Changes: semanticChange("endpoint", "old", true, target.WorkspaceURL())}
			return plannedMutation{plan: plan, apply: func(context.Context) error { return mutationErr }}, nil
		},
	}}
	service := &Service{}
	reviewed, err := service.Plan(context.Background(), client, target)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Apply(context.Background(), reviewed)
	if !errors.Is(err, mutationErr) || !errors.Is(err, verificationErr) {
		t.Fatalf("error = %v, want both mutation and verification causes", err)
	}
}

func TestApplyFailsWhenPostWriteInspectionFails(t *testing.T) {
	target := testTarget(t)
	state := "old"
	inspections := 0
	original := adapters
	t.Cleanup(func() { adapters = original })
	const client ClientID = "reconciliation-test"
	adapters = []adapter{{
		id: client, name: "Reconciliation test", present: func(*Service) (bool, error) { return true, nil },
		planCurrent: func(context.Context, *Service, Target) (plannedMutation, error) {
			inspections++
			if inspections == 3 {
				return plannedMutation{}, errors.New("inspection failed")
			}
			plan := Plan{ConfigPaths: []string{"/tmp/reconciliation"}, Target: target, Changes: semanticChange("endpoint", state, true, target.WorkspaceURL())}
			return plannedMutation{plan: plan, apply: func(context.Context) error {
				state = target.WorkspaceURL()
				return nil
			}}, nil
		},
	}}
	service := &Service{}
	reviewed, err := service.Plan(context.Background(), client, target)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := service.Apply(context.Background(), reviewed)
	if err == nil || !strings.Contains(err.Error(), "written but could not be verified") || verified.ClientID != "" {
		t.Fatalf("verified = %#v, error = %v", verified, err)
	}
}

func reconciliationService(t *testing.T, target Target, state *string, apply func() error) (*Service, Plan) {
	t.Helper()
	original := adapters
	t.Cleanup(func() { adapters = original })
	const client ClientID = "reconciliation-test"
	adapters = []adapter{{
		id: client, name: "Reconciliation test", present: func(*Service) (bool, error) { return true, nil },
		planCurrent: func(context.Context, *Service, Target) (plannedMutation, error) {
			plan := Plan{ConfigPaths: []string{"/tmp/reconciliation"}, Target: target, Changes: semanticChange("endpoint", *state, true, target.WorkspaceURL())}
			mutation := apply
			if mutation == nil {
				mutation = func() error { return nil }
			}
			return plannedMutation{plan: plan, apply: func(context.Context) error { return mutation() }}, nil
		},
	}}
	service := &Service{}
	reviewed, err := service.Plan(context.Background(), client, target)
	if err != nil {
		t.Fatal(err)
	}
	return service, reviewed
}
