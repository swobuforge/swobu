package exchange

import (
	"errors"
	"fmt"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
)

// providerCallAttemptID correlates one issued provider command with its result.
// It is exactly the one-based index of its fact in the exchange-local ledger.
type providerCallAttemptID int

type providerCallAttemptStatus uint8

const (
	providerCallAttemptCalling providerCallAttemptStatus = iota
	providerCallAttemptFailed
	providerCallAttemptHandoffReady
)

type providerCallFailureStage uint8

const (
	providerCallFailureBeforeIngress providerCallFailureStage = iota
	providerCallFailureBeforeHandoff
)

// providerCallFailure records where an issued call stopped while preserving
// the provider-owned typed error as the sole failure taxonomy.
type providerCallFailure struct {
	Stage providerCallFailureStage
	Cause error
}

// providerCallAttempt is compact reducer history for one issued command. Its
// candidate index addresses the immutable route plan; the active phase alone
// retains executable machinery and command correlation identity.
type providerCallAttempt struct {
	candidateIndex int
	target         provider.TargetSnapshot
	requirements   []compat.Feature
	status         providerCallAttemptStatus
	failure        *providerCallFailure
}

func (a providerCallAttempt) terminal() bool {
	return a.status == providerCallAttemptFailed || a.status == providerCallAttemptHandoffReady
}

func providerCallAttemptIndex(s exchangeState, id providerCallAttemptID) (int, bool) {
	index := int(id) - 1
	if index < 0 || index >= len(s.providerCallAttempts) {
		return 0, false
	}
	return index, true
}

func findProviderCallAttempt(s exchangeState, id providerCallAttemptID) (providerCallAttempt, bool) {
	index, ok := providerCallAttemptIndex(s, id)
	if !ok {
		return providerCallAttempt{}, false
	}
	return s.providerCallAttempts[index], true
}

func failProviderCallAttempt(s exchangeState, id providerCallAttemptID, stage providerCallFailureStage, cause error) (exchangeState, error) {
	if cause == nil {
		return exchangeState{}, fmt.Errorf("exchange invariant: provider call attempt %d failed without an error", id)
	}
	index, ok := providerCallAttemptIndex(s, id)
	if !ok {
		return exchangeState{}, fmt.Errorf("exchange invariant: provider call attempt %d is unknown", id)
	}
	if s.providerCallAttempts[index].status != providerCallAttemptCalling {
		return exchangeState{}, fmt.Errorf("exchange invariant: provider call attempt %d is already terminal", id)
	}
	failure := &providerCallFailure{Stage: stage, Cause: cause}
	attempts := append([]providerCallAttempt(nil), s.providerCallAttempts...)
	attempts[index].status = providerCallAttemptFailed
	attempts[index].failure = failure
	s.providerCallAttempts = attempts
	return s, nil
}

func completeProviderCallAttempt(s exchangeState, id providerCallAttemptID) (exchangeState, error) {
	index, ok := providerCallAttemptIndex(s, id)
	if !ok {
		return exchangeState{}, fmt.Errorf("exchange invariant: provider call attempt %d is unknown", id)
	}
	if s.providerCallAttempts[index].status != providerCallAttemptCalling {
		return exchangeState{}, fmt.Errorf("exchange invariant: provider call attempt %d is already terminal", id)
	}
	attempts := append([]providerCallAttempt(nil), s.providerCallAttempts...)
	attempts[index].status = providerCallAttemptHandoffReady
	s.providerCallAttempts = attempts
	return s, nil
}

func beginProviderCallAttempt(s exchangeState, selection providerCallSelection, call providerCall, evidence exchangeEvidence) (reducerOutcome, error) {
	attemptID := providerCallAttemptID(len(s.providerCallAttempts) + 1)
	attempt := providerCallAttempt{
		candidateIndex: selection.candidateIndex, target: call.backend.Target,
		requirements: providerCallRequirements(call),
		status:       providerCallAttemptCalling,
	}
	s.providerCallAttempts = append(append([]providerCallAttempt(nil), s.providerCallAttempts...), attempt)
	s.phase = callingProviderPhase{attemptID: attemptID, call: call}
	return reducerOutcome{
		nextState: s,
		command:   callProviderCommand{attemptID: attemptID, backend: call.backend, document: call.document},
		evidence:  evidence,
	}, nil
}

// providerCallRequirements records only facts that participate in an
// implemented alternative. Future axes add requirements only with their
// concrete call-construction authority.
func providerCallRequirements(call providerCall) []compat.Feature {
	if nativePreviousResponseSent(call.request) {
		return []compat.Feature{compat.RequestPreviousResponseResponses}
	}
	return nil
}

func routeFailoverEligible(err error) bool {
	var unsupported provider.CandidateIncompatibilityError
	var unavailable provider.UnavailableError
	return errors.As(err, &unsupported) || errors.As(err, &unavailable)
}

func backendStatusCode(err error) int {
	var backendErr canonical.BackendError
	if errors.As(err, &backendErr) {
		return backendErr.StatusCode
	}
	return 0
}
