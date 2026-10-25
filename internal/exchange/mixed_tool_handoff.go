package exchange

import (
	"context"
	"fmt"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// finishMixedToolHandoff terminates this Exchange after local work, rather than
// issuing inference while caller-owned results are still pending.
func finishMixedToolHandoff(ctx context.Context, s exchangeState, phase callingMCPPhase, runner runtimeBundle) (reducerOutcome, error) {
	handoff, err := prepareMixedToolHandoff(s.prepared.Request(), phase.response.continuation, phase.calls, phase.results)
	if err != nil {
		s.phase = failedPhase{problem: err, target: phase.target}
		return reducerOutcome{nextState: s}, nil
	}
	usage := canonical.SumTokenUsage(s.providerUsage...)
	call := *phase.handoff
	call.fullRequest = handoff.request
	checkpoint := handoff.response.WithUsage(usage)
	client := handoff.client.WithUsage(usage)
	if call.localToolStreaming && runner.localToolDelivery != nil {
		// Original ordinals identify the events already delivered. The final
		// projector drops bound local calls while checkpoint retains full history.
		client = phase.response.client.WithUsage(usage)
	}
	response := completedProviderResponse{client: client, continuation: checkpoint}
	var output ClientResponse
	if call.localToolStreaming && runner.localToolDelivery != nil {
		runner.localToolDelivery.changes = s.effectiveChanges
		if completion := responseCompletion(runner.localToolDelivery.response); completion != nil {
			completion.ConfigureCompatibility(s.effectiveChanges, nil)
		}
		output, err = runner.localToolDelivery.finish(ctx, call, runner, response)
	} else {
		output, err = handoffCompletedProviderResponse(ctx, call, response, runner)
	}
	if err != nil {
		s.phase = failedPhase{problem: err, target: phase.target}
	} else {
		if completion := responseCompletion(output); completion != nil {
			completion.ConfigureCompatibility(s.effectiveChanges, nil)
		}
		s.phase = completedPhase{response: output, target: phase.target}
	}
	return reducerOutcome{nextState: s}, nil
}

// mixedToolHandoff retains the entire ordered provider round followed by local
// results in checkpoint request history. Its empty checkpoint response marks the
// boundary without putting request-only local results into a provider response.
// The client projection alone removes local calls. Native continuation is removed:
// the provider's round handle does not represent the appended local results.
type mixedToolHandoff struct {
	request  canonical.CanonicalRequest
	response canonical.CanonicalResponse
	client   canonical.CanonicalResponse
}

func prepareMixedToolHandoff(request canonical.CanonicalRequest, response canonical.CanonicalResponse, local []canonical.ToolCallItem, results []canonical.CanonicalItem) (mixedToolHandoff, error) {
	pending := make(map[canonical.ToolCallID]canonical.ToolKey, len(local))
	for _, call := range local {
		if _, duplicate := pending[call.CallID()]; duplicate {
			return mixedToolHandoff{}, fmt.Errorf("mixed handoff repeats a local call ID")
		}
		pending[call.CallID()] = call.Tool()
	}
	if len(pending) == 0 || len(results) != len(pending) {
		return mixedToolHandoff{}, fmt.Errorf("mixed handoff requires one result per local call")
	}
	retained := response.Items()
	var visible []canonical.CanonicalItem
	matched := make(map[canonical.ToolCallID]bool, len(local))
	for _, item := range response.Items() {
		if call, ok := item.ToolCall(); ok {
			if key, owned := pending[call.CallID()]; owned {
				if key != call.Tool() || matched[call.CallID()] {
					return mixedToolHandoff{}, fmt.Errorf("mixed handoff local call binding changed")
				}
				matched[call.CallID()] = true
				continue
			}
		}
		visible = append(visible, item)
	}
	for _, item := range results {
		result, ok := item.ToolResult()
		if !ok || !matched[result.CallID()] {
			return mixedToolHandoff{}, fmt.Errorf("mixed handoff result has no unique local call")
		}
		delete(matched, result.CallID())
		retained = append(retained, item)
	}
	if len(matched) != 0 {
		return mixedToolHandoff{}, fmt.Errorf("mixed handoff has unsettled local calls")
	}
	if err := canonical.ValidateMaterializedRequest(request.WithItems(append(request.Items(), retained...))); err != nil {
		return mixedToolHandoff{}, err
	}
	checkpoint, err := canonical.NewCanonicalResponse(canonical.ResponseRef{SwobuID: response.Response().SwobuID}, response.Model(), nil, response.Completion(), response.Usage())
	if err != nil {
		return mixedToolHandoff{}, err
	}
	client, err := canonical.NewCanonicalResponse(canonical.ResponseRef{SwobuID: response.Response().SwobuID}, response.Model(), visible, response.Completion(), response.Usage())
	if err != nil {
		return mixedToolHandoff{}, err
	}
	return mixedToolHandoff{request: request.WithItems(append(request.Items(), retained...)), response: checkpoint, client: client}, nil
}
