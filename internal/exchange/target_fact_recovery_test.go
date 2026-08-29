package exchange

import (
	"context"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestRejectedAttemptResolvesChangedFactAndReprojectsOnce(t *testing.T) {
	for _, tc := range []struct {
		name, usedName string
		used, resolved bool
	}{
		{name: "stale true", usedName: "true", used: true, resolved: false},
		{name: "stale false", usedName: "false", used: false, resolved: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, runner, attempt := factRecoveryAttempt(t, tc.used)
			failed, err := reduce(context.Background(), s, providerCallFailed{
				attemptID: attempt.id,
				failure: provider.AttemptRejectedBeforeExecution(provider.Rejected(
					canonical.NewBackendError("target", 400, "rejected", ""),
				)),
			}, runner)
			if err != nil {
				t.Fatal(err)
			}
			phase, ok := failed.nextState.phase.(resolvingTargetFactsPhase)
			if !ok || phase.attemptID != attempt.id {
				t.Fatalf("phase = %#v, want fact resolution", failed.nextState.phase)
			}
			resolved, err := reduce(context.Background(), failed.nextState, targetFactsCharacterized{
				attemptID:  attempt.id,
				generation: attempt.call.targetGeneration,
				resolutions: map[provider.TargetFact]bool{
					provider.AcceptsParallelToolCallsFalse: tc.resolved,
				},
			}, runner)
			if err != nil {
				t.Fatal(err)
			}
			reprojected, ok := resolved.nextState.phase.(callingProviderPhase)
			if !ok {
				t.Fatalf("phase = %T, want second provider dispatch", resolved.nextState.phase)
			}
			second, _ := findProviderCallAttempt(resolved.nextState, reprojected.attemptID)
			if second.requestChoice != providerRequestReprojected || len(resolved.nextState.providerCallAttempts) != 2 {
				t.Fatalf("second attempt = %#v", second)
			}
			value, known := runner.TargetExceptions.lookup(attempt.call.targetGeneration, provider.AcceptsParallelToolCallsFalse)
			if tc.resolved && known {
				t.Fatalf("preferred acceptance retained exception = %t/%t from stale %s", value, known, tc.usedName)
			}
			if !tc.resolved && (!known || value) {
				t.Fatalf("preferred rejection = %t/%t, want false/true from stale %s", value, known, tc.usedName)
			}
		})
	}
}

func TestFactRecoveryUsesExistingRecoveryAuthorityAndChangedResolution(t *testing.T) {
	t.Run("round zero possible execution", func(t *testing.T) {
		s, runner, attempt := factRecoveryAttempt(t, true)
		failed, err := reduce(context.Background(), s, providerCallFailed{
			attemptID: attempt.id,
			failure: provider.AttemptMayHaveExecuted(provider.Rejected(
				canonical.NewBackendError("target", 400, "ambiguous", ""),
			)),
		}, runner)
		if err != nil {
			t.Fatal(err)
		}
		if _, resolving := failed.nextState.phase.(resolvingTargetFactsPhase); !resolving {
			t.Fatal("recovery-permitted typed rejection did not enter fact resolution")
		}
	})

	t.Run("round one possible execution", func(t *testing.T) {
		s, runner, attempt := factRecoveryAttempt(t, true)
		index, _ := providerCallAttemptIndex(s, attempt.id)
		s.providerCallAttempts[index].providerRound = 1
		phase := s.phase.(callingProviderPhase)
		phase.call.providerRound = 1
		s.phase = phase
		failed, err := reduce(context.Background(), s, providerCallFailed{
			attemptID: attempt.id,
			failure: provider.AttemptMayHaveExecuted(provider.Rejected(
				canonical.NewBackendError("target", 400, "ambiguous", ""),
			)),
		}, runner)
		if err != nil {
			t.Fatal(err)
		}
		if _, resolving := failed.nextState.phase.(resolvingTargetFactsPhase); resolving {
			t.Fatal("post-effect round entered fact resolution")
		}
	})

	for _, tc := range []struct {
		name       string
		resolution *bool
	}{
		{name: "unchanged", resolution: func() *bool { value := true; return &value }()},
		{name: "inconclusive"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, runner, attempt := factRecoveryAttempt(t, true)
			failed, err := reduce(context.Background(), s, providerCallFailed{
				attemptID: attempt.id,
				failure: provider.AttemptRejectedBeforeExecution(provider.Rejected(
					canonical.NewBackendError("target", 400, "rejected", ""),
				)),
			}, runner)
			if err != nil {
				t.Fatal(err)
			}
			resolutions := map[provider.TargetFact]bool{}
			if tc.resolution != nil {
				resolutions[provider.AcceptsParallelToolCallsFalse] = *tc.resolution
			}
			resolved, err := reduce(context.Background(), failed.nextState, targetFactsCharacterized{
				attemptID:   attempt.id,
				generation:  attempt.call.targetGeneration,
				resolutions: resolutions,
			}, runner)
			if err != nil {
				t.Fatal(err)
			}
			if _, calling := resolved.nextState.phase.(callingProviderPhase); calling {
				t.Fatal("unchanged/inconclusive resolution redispatched")
			}
		})
	}
}

func TestSameCandidateProviderRoundNeverSelectsThirdDispatch(t *testing.T) {
	attempts := []providerCallAttempt{
		{candidateIndex: 0, providerRound: 0, requestChoice: providerRequestPreferred, status: providerCallAttemptFailed},
		{
			candidateIndex: 0, providerRound: 0, requestChoice: providerRequestReprojected,
			nativePreviousResponse: true, status: providerCallAttemptFailed,
			failure: &providerCallFailure{Attempt: provider.AttemptRejectedBeforeExecution(provider.Rejected(
				canonical.NewBackendError("target", 404, "missing", ""),
			))},
		},
	}
	if selection, ok := selectSameTargetAlternative(attempts); ok {
		t.Fatalf("third same-target dispatch selected: %#v", selection)
	}
}

func factRecoveryAttempt(t *testing.T, used bool) (exchangeState, runtimeBundle, activeProviderExecution) {
	t.Helper()
	s := reducerTestState(t)
	target := requestpathTarget(t, "fact-target")
	s.route = routePlan{targets: []routing.Target{target}}
	prepared := mustBeginSession(t, s.input.request)
	s.prepared = &prepared
	runner := reducerRuntime()
	runner.TargetExceptions = newTargetExceptions()
	started, err := advanceProviderExecution(context.Background(), s, runner)
	if err != nil {
		t.Fatal(err)
	}
	attempt := activeProviderAttempt(t, started.nextState)
	reads := map[provider.TargetFact]bool{provider.AcceptsParallelToolCallsFalse: used}
	index, _ := providerCallAttemptIndex(started.nextState, attempt.id)
	started.nextState.providerCallAttempts[index].factReads = cloneFactReads(reads)
	phase := started.nextState.phase.(callingProviderPhase)
	phase.call.factReads = cloneFactReads(reads)
	started.nextState.phase = phase
	attempt.call = phase.call
	return started.nextState, runner, attempt
}
