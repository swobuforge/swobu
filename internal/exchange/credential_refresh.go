package exchange

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
)

// Refresh is a separate effect between terminal invocation evidence and a new
// provider attempt. Neither the failed attempt nor its captured slot is mutated.
type refreshingCredentialPhase struct{ attemptID providerCallAttemptID }

func (refreshingCredentialPhase) isPhase() {}

type credentialRefreshed struct {
	attemptID providerCallAttemptID
	err       error
}

func (credentialRefreshed) isExchangeEvent() {}

type refreshCredentialCommand struct {
	attemptID providerCallAttemptID
	target    provider.TargetSnapshot
	refresh   func(context.Context) (string, error)
	publish   func(context.Context, provider.TargetSnapshot, string) error
}

func (refreshCredentialCommand) isCommand() {}

func credentialRefreshAdmitted(s exchangeState, attempt providerCallAttempt, backend provider.Backend, failure provider.AttemptFailure, runner runtimeBundle) bool {
	if backend.RefreshCredential == nil || runner.PublishRefreshedCredential == nil || failure.Execution() != provider.DispositionRejectedBeforeExecution {
		return false
	}
	if _, _, bound := attempt.target.ConnectionIdentity(); !bound {
		return false
	}
	var backendError canonical.BackendError
	if !errors.As(failure, &backendError) || backendError.StatusCode != http.StatusUnauthorized {
		return false
	}
	// One exchange cannot turn repeated authentication rejection into an
	// unbounded refresh loop, including across alternative request projections.
	for _, prior := range s.providerCallAttempts {
		if prior.candidateIndex == attempt.candidateIndex && prior.credentialRetry {
			return false
		}
	}
	return true
}

func reduceRefreshingCredential(ctx context.Context, s exchangeState, phase refreshingCredentialPhase, event exchangeEvent, runner runtimeBundle) (reducerOutcome, error) {
	result, ok := event.(credentialRefreshed)
	if !ok || result.attemptID != phase.attemptID {
		return reducerOutcome{}, fmt.Errorf("exchange invariant: credential refresh received mismatched event %T", event)
	}
	attempt, ok := findProviderCallAttempt(s, phase.attemptID)
	if !ok {
		return reducerOutcome{}, fmt.Errorf("exchange invariant: credential refresh attempt is missing")
	}
	if result.err != nil {
		return advanceProviderExecution(ctx, s, runner)
	}
	return advanceProviderExecutionFrom(ctx, s, runner, providerCallSelection{candidateIndex: attempt.candidateIndex, requestChoice: attempt.requestChoice, credentialRetry: true})
}
