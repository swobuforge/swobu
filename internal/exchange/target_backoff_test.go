package exchange

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/cachelocality"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestTargetBackoffSkipsUnavailableTargetAcrossExchanges(t *testing.T) {
	slug, _ := routing.ParseWorkspaceSlug("dev")
	routeName, _ := routing.ParseRouteName("a")
	tier, _ := routing.NewTier([]routing.Target{requestpathTarget(t, "a"), requestpathTarget(t, "b")})
	route, _ := routing.NewRoute(routeName, []routing.Tier{tier})
	workspace, err := routing.NewWorkspace(slug, routeName, []routing.Route{route})
	if err != nil {
		t.Fatal(err)
	}
	locality := cachelocality.Explicit("stable-lineage")
	preferred := routing.BuildPlan(locality.Key(), route)[0].ID().String()
	calls := make([]string, 0, 3)
	runner := withRuntime(func(ctx context.Context, target provider.TargetSnapshot, doc carrier.Document) (provider.Ingress, error) {
		calls = append(calls, target.TargetID)
		if target.TargetID == preferred {
			return nil, provider.AttemptMayHaveExecuted(provider.Unavailable(canonical.NewBackendError(preferred, 429, "unavailable", "60")))
		}
		return bufferedProviderTransport(nil)(ctx, target, doc)
	})
	runner.TargetBackoff = newTargetBackoffLedger()
	decoded := testDecodedRequest(testCanonicalRequest("a"))
	decoded.CacheLocality = locality

	for _, exchangeID := range []string{"ex_discovery", "ex_avoids"} {
		if _, err := runExchange(context.Background(), runner, exchangeID, "unknown", canonical.ClientFamilyResponses, delivery.BufferedDelivery(), decoded, nil, workspace, nil, canonical.NormalizedPathResponses); err != nil {
			t.Fatal(err)
		}
	}
	if len(calls) != 3 || calls[0] != preferred || calls[1] == preferred || calls[2] == preferred {
		t.Fatalf("provider calls = %#v, want discovery preferred/fallback then fallback only", calls)
	}
}

func TestEligibleCandidateTraversalPreservesStaticIndicesAndFallbackFact(t *testing.T) {
	s := reducerTestState(t)
	a := requestpathTarget(t, "a")
	b := requestpathTarget(t, "b")
	c := requestpathTarget(t, "c")
	s.route = routePlan{targets: []routing.Target{a, b, c}}
	s.targetBackoff = targetBackoffSnapshot{
		{targetID: "a", targetVersion: uint64(a.Version())}:             {},
		{targetID: b.ID().String(), targetVersion: uint64(b.Version())}: {},
	}
	selection, ok := selectNextProviderCall(s)
	if !ok || selection.candidateIndex != 2 {
		t.Fatalf("first eligible selection = (%#v, %t), want static index 2", selection, ok)
	}
	if hasNextRouteCandidate(s, selection) {
		t.Fatal("terminal eligible target reported a fallback")
	}
	prepared := mustBeginSession(t, s.input.request)
	s.prepared = &prepared
	outcome, err := advanceProviderExecutionFrom(context.Background(), s, reducerRuntime(), selection)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.nextState.evaluatedCandidateCount != 3 {
		t.Fatalf("evaluated candidates = %d, want 3", outcome.nextState.evaluatedCandidateCount)
	}
}

func TestHasNextRouteCandidateSkipsSuppressedCandidateAndFindsEligibleFallback(t *testing.T) {
	s := reducerTestState(t)
	a := requestpathTarget(t, "a")
	b := requestpathTarget(t, "b")
	c := requestpathTarget(t, "c")
	s.route = routePlan{targets: []routing.Target{a, b, c}}
	s.targetBackoff = targetBackoffSnapshot{
		{targetID: "b", targetVersion: uint64(b.Version())}: {},
	}
	if !hasNextRouteCandidate(s, providerCallSelection{candidateIndex: 0, requestChoice: providerRequestPreferred}) {
		t.Fatal("eligible C after suppressed B was not exposed as fallback from A")
	}
}

func TestAllBackedOffTargetsReturnNoAvailableTarget(t *testing.T) {
	s := reducerTestState(t)
	prepared := mustBeginSession(t, s.input.request)
	s.prepared = &prepared
	target := requestpathTarget(t, "a")
	s.route = routePlan{targets: []routing.Target{target}}
	s.targetBackoff = targetBackoffSnapshot{
		{targetID: "a", targetVersion: uint64(target.Version())}: {},
	}
	outcome, err := advanceProviderExecution(context.Background(), s, reducerRuntime())
	if err != nil {
		t.Fatal(err)
	}
	failed, ok := outcome.nextState.phase.(failedPhase)
	if !ok {
		t.Fatalf("phase = %T, want failedPhase", outcome.nextState.phase)
	}
	var canonicalErr canonical.Error
	if !errors.As(failed.problem, &canonicalErr) || canonicalErr.Code != canonical.ErrorCodeNoAvailableTarget {
		t.Fatalf("failure = %#v, want NO_AVAILABLE_TARGET", failed.problem)
	}
}

func TestSuppressedCandidateDoesNotSkipEligibleProjectedTarget(t *testing.T) {
	s := reducerTestState(t)
	prepared := mustBeginSession(t, s.input.request)
	s.prepared = &prepared
	a := requestpathTarget(t, "a")
	b := requestpathTarget(t, "b")
	s.route = routePlan{targets: []routing.Target{a, b}}
	s.targetBackoff = targetBackoffSnapshot{
		{targetID: "a", targetVersion: uint64(a.Version())}: {},
	}
	runner := withRuntime(bufferedProviderTransport(nil))

	outcome, err := advanceProviderExecution(context.Background(), s, runner)
	if err != nil {
		t.Fatal(err)
	}
	attempt := activeProviderAttempt(t, outcome.nextState)
	if attempt.candidateIndex != 1 || attempt.target.TargetID != "b" {
		t.Fatalf("attempt = %#v, want eligible target b", attempt)
	}
}

func TestSuppressedCandidateDoesNotLaunderEligibleBackendTerminal(t *testing.T) {
	for _, status := range []int{401, 429, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			s := reducerTestState(t)
			a := requestpathTarget(t, "a")
			b := requestpathTarget(t, "b")
			s.route = routePlan{targets: []routing.Target{a, b}}
			s.targetBackoff = targetBackoffSnapshot{
				{targetID: "a", targetVersion: uint64(a.Version())}: {},
			}
			backendErr := canonical.NewBackendError("b", status, "backend terminal", "")
			failure := provider.AttemptMayHaveExecuted(provider.NormalizeFailure(backendErr))
			s.providerCallAttempts = []providerCallAttempt{{
				candidateIndex: 1,
				target:         providerSnapshotForTarget(t, b),
				status:         providerCallAttemptFailed,
				failure:        &providerCallFailure{Attempt: failure},
			}}

			outcome := terminateProviderExecution(s)
			failed := outcome.nextState.phase.(failedPhase)
			var swobuErr canonical.Error
			if errors.As(failed.problem, &swobuErr) {
				t.Fatalf("backend status %d laundered into %s", status, swobuErr.Code)
			}
			var gotBackend canonical.BackendError
			if !errors.As(failed.problem, &gotBackend) || gotBackend.StatusCode != status {
				t.Fatalf("failure = %#v, want backend status %d", failed.problem, status)
			}
		})
	}
}

func TestTargetBackoffSnapshotIsolatedByWorkspaceAndGeneration(t *testing.T) {
	now := time.Date(2026, 8, 19, 8, 0, 0, 0, time.UTC)
	ledger := newTargetBackoffLedger()
	ledger.now = func() time.Time { return now }
	target := requestpathTarget(t, "a")
	snapshot := providerSnapshotForTarget(t, target)
	observeTarget(ledger, "dev", snapshot, unavailableEvent(snapshot.TargetID, "60"))

	if !ledger.snapshot("dev").active(target) {
		t.Fatal("observed exact target generation is eligible")
	}
	if ledger.snapshot("prod").active(target) {
		t.Fatal("observation crossed workspace boundary")
	}
	if _, found := ledger.snapshot("dev")[targetGeneration{targetID: target.ID().String(), targetVersion: uint64(target.Version()) + 1}]; found {
		t.Fatal("observation crossed target generation boundary")
	}
}

func TestTargetBackoffUsesExponentialDelayAndRetryAfterFloor(t *testing.T) {
	now := time.Date(2026, 8, 19, 8, 0, 0, 0, time.UTC)
	ledger := newTargetBackoffLedger()
	ledger.now = func() time.Time { return now }
	target := provider.TargetSnapshot{TargetID: "a", TargetVersion: 1}
	key := targetBackoffKey{workspace: "dev", targetID: "a", targetVersion: 1}

	observeTarget(ledger, "dev", target, unavailableEvent("a", ""))
	if got := ledger.records[key].until.Sub(now); got != time.Second {
		t.Fatalf("first delay = %s, want 1s", got)
	}
	now = ledger.records[key].until
	observeTarget(ledger, "dev", target, unavailableEvent("a", "60"))
	if got := ledger.records[key].until.Sub(now); got != time.Minute {
		t.Fatalf("Retry-After floor = %s, want 1m", got)
	}
	if got := ledger.records[key].delay; got != 2*time.Second {
		t.Fatalf("Retry-After changed local delay to %s, want 2s", got)
	}
	for range 20 {
		now = ledger.records[key].until
		observeTarget(ledger, "dev", target, unavailableEvent("a", ""))
	}
	if got := ledger.records[key].until.Sub(now); got != maximumTargetBackoff {
		t.Fatalf("capped delay = %s, want %s", got, maximumTargetBackoff)
	}
}

func TestTargetBackoffConcurrentEpochFailuresDoNotAccelerateDelay(t *testing.T) {
	now := time.Date(2026, 8, 19, 8, 0, 0, 0, time.UTC)
	ledger := newTargetBackoffLedger()
	ledger.now = func() time.Time { return now }
	target := provider.TargetSnapshot{TargetID: "a", TargetVersion: 1}
	key := targetBackoffKey{workspace: "dev", targetID: "a", targetVersion: 1}

	observeTarget(ledger, "dev", target, unavailableEvent("a", ""))
	firstUntil := ledger.records[key].until
	var group sync.WaitGroup
	for range 20 {
		group.Add(1)
		go func() {
			defer group.Done()
			observeTarget(ledger, "dev", target, unavailableEvent("a", ""))
		}()
	}
	group.Wait()
	if record := ledger.records[key]; record.delay != time.Second || !record.until.Equal(firstUntil) {
		t.Fatalf("same-epoch failures accelerated record to delay=%s until=%s, want 1s/%s", record.delay, record.until, firstUntil)
	}

	observeTarget(ledger, "dev", target, unavailableEvent("a", "60"))
	if record := ledger.records[key]; record.delay != time.Second || record.until.Sub(now) != time.Minute {
		t.Fatalf("same-epoch Retry-After = delay %s until %s, want 1s/1m", record.delay, record.until.Sub(now))
	}

	now = ledger.records[key].until
	observeTarget(ledger, "dev", target, unavailableEvent("a", ""))
	if record := ledger.records[key]; record.delay != 2*time.Second || record.until.Sub(now) != 2*time.Second {
		t.Fatalf("next probe epoch = delay %s until %s, want 2s/2s", record.delay, record.until.Sub(now))
	}
}

func TestTargetBackoffSlowPreEpochFailureDoesNotAdvanceAfterExpiry(t *testing.T) {
	now := time.Date(2026, 8, 19, 8, 0, 0, 0, time.UTC)
	ledger := newTargetBackoffLedger()
	ledger.now = func() time.Time { return now }
	target := provider.TargetSnapshot{TargetID: "a", TargetVersion: 1}
	key := targetBackoffKey{workspace: "dev", targetID: "a", targetVersion: 1}

	first := ledger.begin("dev", target)
	slow := ledger.begin("dev", target)
	ledger.observe(first, unavailableEvent("a", ""))
	now = ledger.records[key].until.Add(time.Second)
	ledger.observe(slow, unavailableEvent("a", ""))

	if record := ledger.records[key]; record.delay != time.Second {
		t.Fatalf("pre-epoch failure advanced delay to %s after expiry, want 1s", record.delay)
	}
}

func TestTargetBackoffNewerSuccessPreventsOlderFailureFromResuppressing(t *testing.T) {
	ledger := newTargetBackoffLedger()
	target := provider.TargetSnapshot{TargetID: "a", TargetVersion: 1}
	key := targetBackoffKey{workspace: "dev", targetID: "a", targetVersion: 1}
	olderFailure := ledger.begin("dev", target)
	newerSuccess := ledger.begin("dev", target)

	ledger.observe(newerSuccess, providerIngressReceived{})
	ledger.observe(olderFailure, unavailableEvent("a", ""))

	if _, ok := ledger.records[key]; ok {
		t.Fatal("older failure re-suppressed target after newer success")
	}
}

func TestTargetBackoffNewerUnavailablePreventsOlderSuccessFromClearing(t *testing.T) {
	ledger := newTargetBackoffLedger()
	target := provider.TargetSnapshot{TargetID: "a", TargetVersion: 1}
	key := targetBackoffKey{workspace: "dev", targetID: "a", targetVersion: 1}
	olderSuccess := ledger.begin("dev", target)
	newerFailure := ledger.begin("dev", target)

	ledger.observe(newerFailure, unavailableEvent("a", ""))
	ledger.observe(olderSuccess, providerIngressReceived{})

	if record, ok := ledger.records[key]; !ok || record.delay != time.Second {
		t.Fatalf("older success cleared newer unavailability: record=%#v present=%t", record, ok)
	}
}

func TestTargetBackoffPostExpiryAdmissionAdvancesExactlyOnce(t *testing.T) {
	now := time.Date(2026, 8, 19, 8, 0, 0, 0, time.UTC)
	ledger := newTargetBackoffLedger()
	ledger.now = func() time.Time { return now }
	target := provider.TargetSnapshot{TargetID: "a", TargetVersion: 1}
	key := targetBackoffKey{workspace: "dev", targetID: "a", targetVersion: 1}

	first := ledger.begin("dev", target)
	ledger.observe(first, unavailableEvent("a", ""))
	now = ledger.records[key].until
	probe := ledger.begin("dev", target)
	concurrent := ledger.begin("dev", target)
	ledger.observe(probe, unavailableEvent("a", ""))
	ledger.observe(concurrent, unavailableEvent("a", ""))

	if record := ledger.records[key]; record.delay != 2*time.Second || record.until.Sub(now) != 2*time.Second {
		t.Fatalf("post-expiry probe epoch = delay %s until %s, want 2s/2s", record.delay, record.until.Sub(now))
	}
}

func TestTargetBackoffOpportunisticallyCleansStaleRecords(t *testing.T) {
	now := time.Date(2026, 8, 19, 8, 0, 0, 0, time.UTC)
	ledger := newTargetBackoffLedger()
	ledger.now = func() time.Time { return now }
	stale := targetBackoffKey{workspace: "dev", targetID: "old", targetVersion: 1}
	active := targetBackoffKey{workspace: "dev", targetID: "active", targetVersion: 1}
	ledger.records[stale] = targetBackoffRecord{delay: time.Second, until: now.Add(-targetBackoffStale)}
	ledger.records[active] = targetBackoffRecord{delay: time.Second, until: now.Add(time.Minute)}

	snapshot := ledger.snapshot("dev")
	if _, ok := ledger.records[stale]; ok {
		t.Fatal("stale backoff record was not reclaimed")
	}
	if _, ok := ledger.observations[stale]; ok {
		t.Fatal("stale observation order was not reclaimed")
	}
	if _, ok := ledger.records[active]; !ok || len(snapshot) != 1 {
		t.Fatalf("active record or snapshot lost: records=%#v snapshot=%#v", ledger.records, snapshot)
	}
}

func TestTargetBackoffReachabilityClearsHistory(t *testing.T) {
	ledger := newTargetBackoffLedger()
	target := provider.TargetSnapshot{TargetID: "a", TargetVersion: 1}
	key := targetBackoffKey{workspace: "dev", targetID: "a", targetVersion: 1}
	observeTarget(ledger, "dev", target, unavailableEvent("a", ""))
	observeTarget(ledger, "dev", target, providerIngressReceived{})
	if _, ok := ledger.records[key]; ok {
		t.Fatal("successful ingress retained failure history")
	}
	observeTarget(ledger, "dev", target, unavailableEvent("a", ""))
	observeTarget(ledger, "dev", target, providerCallFailed{failure: provider.AttemptRejectedBeforeExecution(provider.Rejected(canonical.NewBackendError("a", 401, "rejected", "")))})
	if _, ok := ledger.records[key]; ok {
		t.Fatal("backend rejection retained failure history")
	}
}

func TestTargetBackoffConcurrentObservationAndSnapshot(t *testing.T) {
	ledger := newTargetBackoffLedger()
	target := provider.TargetSnapshot{TargetID: "a", TargetVersion: 1}
	var group sync.WaitGroup
	for range 20 {
		group.Add(2)
		go func() {
			defer group.Done()
			observeTarget(ledger, "dev", target, unavailableEvent("a", ""))
		}()
		go func() {
			defer group.Done()
			_ = ledger.snapshot("dev")
		}()
	}
	group.Wait()
}

func unavailableEvent(targetID, retryAfter string) providerCallFailed {
	return providerCallFailed{failure: provider.AttemptMayHaveExecuted(provider.Unavailable(
		canonical.NewBackendError(targetID, 429, "unavailable", retryAfter),
	))}
}

func observeTarget(ledger *targetBackoffLedger, workspace string, target provider.TargetSnapshot, event exchangeEvent) {
	ledger.observe(ledger.begin(workspace, target), event)
}

func providerSnapshotForTarget(t *testing.T, target routing.Target) provider.TargetSnapshot {
	t.Helper()
	return provider.TargetSnapshot{TargetID: target.ID().String(), TargetVersion: uint64(target.Version())}
}
