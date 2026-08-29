package exchange

import (
	"fmt"

	"github.com/swobuforge/swobu/internal/compat"
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

type providerCallFailure struct {
	Attempt provider.AttemptFailure
}

// providerCallAttempt is compact reducer history for one issued command. Its
// candidate index addresses the immutable route plan; the active phase alone
// retains executable machinery and command correlation identity.
type providerCallAttempt struct {
	candidateIndex         int
	target                 provider.TargetSnapshot
	requestChoice          providerRequestChoice
	providerRound          int
	nativePreviousResponse bool
	targetGeneration       targetExceptionGeneration
	factReads              map[provider.TargetFact]bool
	requestChanges         []compat.Change
	status                 providerCallAttemptStatus
	failure                *providerCallFailure
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

func failProviderCallAttempt(s exchangeState, id providerCallAttemptID, failure provider.AttemptFailure) (exchangeState, error) {
	if _, ok := provider.AsAttemptFailure(failure); !ok {
		return exchangeState{}, fmt.Errorf("exchange invariant: provider call attempt %d has invalid failure facts", id)
	}
	index, ok := providerCallAttemptIndex(s, id)
	if !ok {
		return exchangeState{}, fmt.Errorf("exchange invariant: provider call attempt %d is unknown", id)
	}
	if s.providerCallAttempts[index].status != providerCallAttemptCalling {
		return exchangeState{}, fmt.Errorf("exchange invariant: provider call attempt %d is already terminal", id)
	}
	attempts := append([]providerCallAttempt(nil), s.providerCallAttempts...)
	attempts[index].status = providerCallAttemptFailed
	attempts[index].failure = &providerCallFailure{Attempt: failure}
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

func beginProviderCallAttempt(s exchangeState, selection providerCallSelection, call providerCall, requestChanges []compat.Change) (reducerOutcome, error) {
	if err := compat.ValidateChanges(requestChanges); err != nil {
		return reducerOutcome{}, fmt.Errorf("exchange invariant: provider request changes are invalid: %w", err)
	}
	for _, prior := range s.providerCallAttempts {
		if prior.candidateIndex == selection.candidateIndex &&
			prior.requestChoice == selection.requestChoice &&
			prior.providerRound == call.providerRound {
			return reducerOutcome{}, fmt.Errorf(
				"exchange invariant: provider call operation was already consumed for candidate %d choice %d round %d",
				selection.candidateIndex, selection.requestChoice, call.providerRound,
			)
		}
	}
	attemptID := providerCallAttemptID(len(s.providerCallAttempts) + 1)
	attempt := providerCallAttempt{
		candidateIndex: selection.candidateIndex, target: call.backend.Target,
		requestChoice: selection.requestChoice, providerRound: call.providerRound,
		nativePreviousResponse: nativePreviousResponseSent(call.request),
		targetGeneration:       call.targetGeneration,
		factReads:              cloneFactReads(call.factReads),
		requestChanges:         compat.CloneChanges(requestChanges),
		status:                 providerCallAttemptCalling,
	}
	s.providerCallAttempts = append(append([]providerCallAttempt(nil), s.providerCallAttempts...), attempt)
	s.phase = callingProviderPhase{attemptID: attemptID, call: call}
	return reducerOutcome{
		nextState: s,
		command:   callProviderCommand{attemptID: attemptID, backend: call.backend, document: call.document},
	}, nil
}
