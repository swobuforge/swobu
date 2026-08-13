package exchange

import (
	"context"
	"fmt"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func beginMCPBatch(s exchangeState, phase callingMCPPhase) (reducerOutcome, error) {
	if len(phase.calls) == 0 {
		return reducerOutcome{}, canonical.InternalError("MCP call batch is empty")
	}
	if s.mcp == nil {
		return reducerOutcome{}, canonical.InternalError("MCP call batch requires a request-scoped runtime")
	}
	s.phase = phase
	return reducerOutcome{
		nextState: s,
		command: beginMCPBatchCommand{
			run: s.mcp, calls: append([]canonical.ToolCallItem(nil), phase.calls...),
		},
	}, nil
}

func beginMCPCall(s exchangeState, phase callingMCPPhase) (reducerOutcome, error) {
	if phase.next < 0 || phase.next >= len(phase.calls) {
		return reducerOutcome{}, fmt.Errorf("exchange invariant: MCP call index is outside the fixed batch")
	}
	if s.mcp == nil {
		return reducerOutcome{}, canonical.InternalError("MCP call requires a request-scoped runtime")
	}
	call := phase.calls[phase.next]
	s.phase = phase
	return reducerOutcome{
		nextState: s,
		command:   callMCPCommand{run: s.mcp, call: call},
	}, nil
}

func reduceCallingMCP(ctx context.Context, s exchangeState, phase callingMCPPhase, event exchangeEvent, runner runtimeBundle) (reducerOutcome, error) {
	if started, ok := event.(mcpBatchStarted); ok {
		if started.err != nil {
			s.phase = failedPhase{problem: started.err, target: phase.target}
			return reducerOutcome{nextState: s}, nil
		}
		s.polyfilled = true
		return beginMCPCall(s, phase)
	}
	returned, ok := event.(mcpToolReturned)
	if !ok {
		return reducerOutcome{}, fmt.Errorf("exchange invariant: calling MCP received %T", event)
	}
	if returned.err != nil {
		s.phase = failedPhase{problem: returned.err, target: phase.target}
		return reducerOutcome{nextState: s}, nil
	}
	phase.results = append(phase.results, returned.result)
	phase.next++
	if phase.next < len(phase.calls) {
		return beginMCPCall(s, phase)
	}
	next, replaceErr := s.prepared.ContinueAfterLocalResult(phase.response, phase.results)
	if replaceErr != nil {
		s.phase = failedPhase{problem: canonical.InternalError("MCP tool loop produced invalid complete history: " + replaceErr.Error()), target: phase.target}
		return reducerOutcome{nextState: s}, nil
	}
	s.prepared = &next
	call, target, requestChanges, fetchCache, err := prepareProviderCall(ctx, s, phase.selection, runner)
	s.mediaFetchCache = cloneMediaFetchCache(fetchCache)
	if err != nil {
		s.phase = failedPhase{problem: err, target: target}
		return reducerOutcome{nextState: s}, nil
	}
	return beginProviderCallAttempt(s, phase.selection, call, requestChanges)
}
