package exchange

import (
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

type providerCallFailure struct {
	Attempt provider.AttemptFailure
}

type providerReplaySafety uint8

const (
	providerReplaySafe providerReplaySafety = iota + 1
	providerReplayUnsafe
)

// providerReplaySafetyFor classifies the final semantic provider attempt.
// Ordinary inference and caller-executed tools are safe to repeat. A remaining
// native MCP source can execute outside Swobu, so uncertain delivery fails
// closed without a parallel effect registry.
func providerReplaySafetyFor(request canonical.CanonicalRequest) (providerReplaySafety, error) {
	environment, err := canonical.EffectiveTools(request)
	if err != nil {
		return 0, err
	}
	var inspect func(canonical.ToolDeclaration) (providerReplaySafety, error)
	inspect = func(declaration canonical.ToolDeclaration) (providerReplaySafety, error) {
		switch declaration.Kind() {
		case canonical.ToolKindFunction, canonical.ToolKindCustom,
			canonical.ToolKindWebSearch, canonical.ToolKindDiscovery:
			return providerReplaySafe, nil
		case canonical.ToolKindMCP:
			return providerReplayUnsafe, nil
		case canonical.ToolKindNamespace:
			namespace, ok := declaration.Namespace()
			if !ok {
				return 0, fmt.Errorf("canonical tool namespace branch is invalid")
			}
			for _, child := range namespace.Tools() {
				safety, err := inspect(child)
				if err != nil || safety == providerReplayUnsafe {
					return safety, err
				}
			}
			return providerReplaySafe, nil
		default:
			return 0, fmt.Errorf("canonical tool declaration has unknown execution kind %q", declaration.Kind())
		}
	}
	for _, declaration := range environment.Declarations() {
		safety, err := inspect(declaration)
		if err != nil || safety == providerReplayUnsafe {
			return safety, err
		}
	}
	return providerReplaySafe, nil
}

// providerCallAttempt is compact reducer history for one issued command. Its
// candidate index addresses the immutable route plan; the active phase alone
// retains executable machinery and command correlation identity.
type providerCallAttempt struct {
	candidateIndex         int
	target                 provider.TargetSnapshot
	requestChoice          providerRequestChoice
	providerRound          int
	retry                  bool
	replaySafety           providerReplaySafety
	nativePreviousResponse bool
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
			prior.providerRound == call.providerRound &&
			prior.retry == selection.retry {
			return reducerOutcome{}, fmt.Errorf(
				"exchange invariant: provider call attempt key was already consumed for candidate %d choice %d round %d retry %t",
				selection.candidateIndex, selection.requestChoice, call.providerRound, selection.retry,
			)
		}
	}
	attemptID := providerCallAttemptID(len(s.providerCallAttempts) + 1)
	attempt := providerCallAttempt{
		candidateIndex: selection.candidateIndex, target: call.backend.Target,
		requestChoice: selection.requestChoice, providerRound: call.providerRound,
		retry: selection.retry, replaySafety: call.replaySafety,
		nativePreviousResponse: nativePreviousResponseSent(call.request),
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
