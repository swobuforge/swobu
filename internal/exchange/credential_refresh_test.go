package exchange

import (
	"context"
	"errors"
	"github.com/swobuforge/swobu/internal/accounting"
	"path/filepath"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/connections"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/provider"
)

func TestCredentialRefreshIssuesSeparateRevisionBoundAttempt(t *testing.T) {
	started := startReducerProviderCall(t, reducerTestState(t))
	active := activeProviderAttempt(t, started.nextState)
	id, revision, bound := active.target.ConnectionIdentity()
	if !bound {
		t.Fatal("fixture lacks Connection")
	}
	runner := reducerRuntime()
	accountStore, err := accounting.Open(filepath.Join(t.TempDir(), "accounting.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := accountStore.SetLimit("dev", accounting.Limit{Currency: "USD", AmountCents: 100, Window: "day", Mode: accounting.ModeObserveOnly, UnknownPrice: "reject"}); err != nil {
		t.Fatal(err)
	}
	runner.Accounting = accountStore
	// Re-issue initial preparation with accounting installed so both operations
	// must reserve distinct identities despite selecting the same target/round.
	state := reducerTestState(t)
	state.route = started.nextState.route
	state.prepared = started.nextState.prepared
	started, err = advanceProviderExecution(t.Context(), state, runner)
	if err != nil {
		t.Fatal(err)
	}
	active = activeProviderAttempt(t, started.nextState)
	id, revision, bound = active.target.ConnectionIdentity()
	current, err := runner.ModelConnection(id)
	if err != nil {
		t.Fatal(err)
	}
	runner.ModelConnection = func(got connectionidentity.ID) (connections.Connection, error) {
		if got == id {
			return current, nil
		}
		return LookupModelFixture(got)
	}
	refreshCalls, publications := 0, 0
	phase := started.nextState.phase.(callingProviderPhase)
	phase.call.backend.RefreshCredential = func(context.Context) (string, error) { refreshCalls++; return "synthetic-bundle", nil }
	started.nextState.phase = phase
	runner.PublishRefreshedCredential = func(ctx context.Context, target provider.TargetSnapshot, material string) error {
		publications++
		if !target.Equal(active.target) || material != "synthetic-bundle" {
			t.Fatal("refresh lost captured input")
		}
		var err error
		current, err = current.EditSameIdentity(current.Revision(), current.DisplayName(), current.Spec(), time.Now().Add(time.Second))
		return err
	}
	failure := provider.AttemptRejectedBeforeExecution(canonical.NewBackendError(active.target.TargetID, 401, "rejected", ""))
	refreshing, err := reduce(t.Context(), started.nextState, providerCallFailed{attemptID: active.id, failure: failure}, runner)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := refreshing.command.(refreshCredentialCommand); !ok {
		t.Fatalf("command %T", refreshing.command)
	}
	if refreshCalls != 0 || publications != 0 {
		t.Fatal("reducer performed refresh I/O")
	}
	retried, err := reduce(t.Context(), refreshing.nextState, executeCommand(t.Context(), refreshing.command), runner)
	if err != nil {
		t.Fatal(err)
	}
	if failed, ok := retried.nextState.phase.(failedPhase); ok {
		t.Fatalf("retry failed: %v", failed.problem)
	}
	after := activeProviderAttempt(t, retried.nextState)
	_, nextRevision, _ := after.target.ConnectionIdentity()
	if len(retried.nextState.providerCallAttempts) != 2 || after.id == active.id || nextRevision != revision+1 || after.target.TargetVersion != active.target.TargetVersion || refreshCalls != 1 || publications != 1 {
		t.Fatal("retry did not retain independent attempt identity")
	}
	reservations := accountStore.Reservations("dev")
	if len(reservations) != 2 || reservations[0].Operation == reservations[1].Operation {
		t.Fatal("refresh reused original accounting operation")
	}
	old := retried.nextState.providerCallAttempts[0]
	if !old.target.Equal(active.target) || old.status != providerCallAttemptFailed {
		t.Fatal("old attempt was rewritten")
	}
	// A second rejection cannot refresh again, even with a refresh-capable backend.
	retryPhase := retried.nextState.phase.(callingProviderPhase)
	retryPhase.call.backend.RefreshCredential = phase.call.backend.RefreshCredential
	retried.nextState.phase = retryPhase
	terminal, err := reduce(t.Context(), retried.nextState, providerCallFailed{attemptID: after.id, failure: failure}, runner)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := terminal.command.(refreshCredentialCommand); ok {
		t.Fatal("refresh loop")
	}
}

func TestRefreshCommandStopsBeforePublicationOnFailureOrCancellation(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		ctx, cancel := context.WithCancel(t.Context())
		calls := 0
		denied := errors.New("synthetic refresh failed")
		command := refreshCredentialCommand{refresh: func(context.Context) (string, error) { calls++; return "", denied }, publish: func(context.Context, provider.TargetSnapshot, string) error {
			t.Fatal("published failed refresh")
			return nil
		}}
		if cancelled {
			cancel()
		}
		result := executeCommand(ctx, command).(credentialRefreshed)
		cancel()
		if result.err == nil || cancelled && (calls != 0 || !errors.Is(result.err, context.Canceled)) {
			t.Fatal("lost failure or cancellation")
		}
	}
}

func TestCredentialRefreshAdmissionRequiresPositiveAuthenticationRejection(t *testing.T) {
	started := startReducerProviderCall(t, reducerTestState(t))
	active := activeProviderAttempt(t, started.nextState)
	backend := active.call.backend
	backend.RefreshCredential = func(context.Context) (string, error) { return "fixture", nil }
	runner := reducerRuntime()
	runner.PublishRefreshedCredential = func(context.Context, provider.TargetSnapshot, string) error { return nil }
	for _, tc := range []struct {
		name    string
		failure provider.AttemptFailure
		want    bool
	}{
		{"positive 401", provider.AttemptRejectedBeforeExecution(canonical.NewBackendError(active.target.TargetID, 401, "", "")), true},
		{"uncertain 401", provider.AttemptMayHaveExecuted(canonical.NewBackendError(active.target.TargetID, 401, "", "")), false},
		{"local 401", provider.AttemptNotDispatched(canonical.NewBackendError(active.target.TargetID, 401, "", "")), false},
		{"permission rejection", provider.AttemptRejectedBeforeExecution(canonical.NewBackendError(active.target.TargetID, 403, "", "")), false},
		{"prose only", provider.AttemptRejectedBeforeExecution(errors.New("401 unauthorized")), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := credentialRefreshAdmitted(started.nextState, active.providerCallAttempt, backend, tc.failure, runner); got != tc.want {
				t.Fatalf("admitted=%v", got)
			}
		})
	}
}
