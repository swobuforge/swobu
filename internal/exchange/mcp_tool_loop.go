package exchange

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/swobuforge/swobu/internal/continuity"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/domain/execution"
	"github.com/swobuforge/swobu/internal/domain/executionevidence"
	mcpruntime "github.com/swobuforge/swobu/internal/mcp"
	"github.com/swobuforge/swobu/internal/observation"
	"github.com/swobuforge/swobu/internal/provider"
	"time"
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

func beginMCPCall(ctx context.Context, s exchangeState, phase callingMCPPhase) (reducerOutcome, error) {
	if err := ctx.Err(); err != nil {
		s.phase = failedPhase{problem: err, target: phase.target}
		return reducerOutcome{nextState: s}, nil
	}
	if phase.next < 0 || phase.next >= len(phase.calls) {
		return reducerOutcome{}, fmt.Errorf("exchange invariant: MCP call index is outside the fixed batch")
	}
	if s.mcp == nil {
		return reducerOutcome{}, canonical.InternalError("MCP call requires a request-scoped runtime")
	}
	prepared, err := s.mcp.PrepareCall(phase.calls[phase.next])
	if err != nil {
		return reducerOutcome{}, err
	}
	source, tool, ok := prepared.ExecutionIdentity()
	if !ok {
		return reducerOutcome{}, canonical.InternalError("prepared MCP call has no execution identity")
	}
	resource, err := executionevidence.NewMCPResource(source, tool)
	if err != nil {
		return reducerOutcome{}, canonical.InternalError("prepared MCP call has invalid execution identity")
	}
	if connectionID, connectionRevision, bound := prepared.ConnectionIdentity(); bound {
		id, parseErr := connectionidentity.Parse(connectionID)
		revision, revisionErr := strconv.ParseUint(connectionRevision, 10, 64)
		if parseErr != nil || revisionErr != nil || revision == 0 {
			return reducerOutcome{}, canonical.InternalError("prepared MCP call has invalid Connection identity")
		}
		resource, err = resource.BindConnection(id, revision)
		if err != nil {
			return reducerOutcome{}, canonical.InternalError("prepared MCP call could not bind Connection identity")
		}
	}
	if exchangeID := s.inputExchangeID(); exchangeID.Valid() {
		if s.invocationOrdinal != nil {
			*s.invocationOrdinal++
		}
		if s.invocationOrdinal != nil {
			if invocationID, err := execution.NewInvocationID(exchangeID, *s.invocationOrdinal); err == nil {
				phase.invocationID = invocationID
			}
		}
	}
	s.phase = phase
	return reducerOutcome{
		nextState: s,
		command:   callMCPCommand{prepared: prepared, resource: resource},
	}, nil
}

func reduceCallingMCP(ctx context.Context, s exchangeState, phase callingMCPPhase, event exchangeEvent, runner runtimeBundle) (reducerOutcome, error) {
	if started, ok := event.(mcpBatchStarted); ok {
		if started.err != nil {
			s.phase = failedPhase{problem: started.err, target: phase.target}
			return reducerOutcome{nextState: s}, nil
		}
		return beginMCPCall(ctx, s, phase)
	}
	returned, ok := event.(mcpToolReturned)
	if !ok {
		return reducerOutcome{}, fmt.Errorf("exchange invariant: calling MCP received %T", event)
	}
	if returned.err != nil {
		if errors.Is(returned.err, context.Canceled) {
			emitMCPSettlement(runner.ExecutionEvidence, s, phase, returned, execution.Cancelled, execution.MayHaveOccurred, "")
			s.phase = failedPhase{problem: returned.err, target: phase.target}
			return reducerOutcome{nextState: s}, nil
		}
		failure := executionevidence.FailureMCPTransport
		possibility := provider.DispositionMayHaveOccurred
		var projectionErr *mcpruntime.ResultProjectionError
		if errors.As(returned.err, &projectionErr) {
			failure = executionevidence.FailureResultProjection
			possibility = provider.DispositionMayHaveOccurred
		}
		emitMCPSettlement(runner.ExecutionEvidence, s, phase, returned, execution.Failed, occurrenceFromDisposition(possibility), failure)
		s.phase = failedPhase{problem: returned.err, target: phase.target}
		return reducerOutcome{nextState: s}, nil
	}
	outcome, failure, ok := classifyMCPResult(returned.result)
	if !ok {
		// The MCP adapter must return a canonical tool result. Treat any other
		// item as an internal projection failure; the boolean returned by
		// ToolResult reports item kind, not the tool's remote error bit.
		emitMCPSettlement(runner.ExecutionEvidence, s, phase, returned, execution.Failed, execution.Occurred, executionevidence.FailureResultProjection)
		s.phase = failedPhase{problem: canonical.InternalError("MCP execution returned a non-tool-result item"), target: phase.target}
		return reducerOutcome{nextState: s}, nil
	}
	emitMCPSettlement(runner.ExecutionEvidence, s, phase, returned, outcome, execution.Occurred, failure)
	phase.results = append(phase.results, returned.result)
	phase.next++
	if phase.next < len(phase.calls) {
		return beginMCPCall(ctx, s, phase)
	}
	if phase.handoff != nil {
		return finishMixedToolHandoff(ctx, s, phase, runner)
	}
	next, replaceErr := continueAfterLocalResults(*s.prepared, phase.response, phase.results)
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
	return beginProviderCallAttempt(s, phase.selection, call, requestChanges, runner)
}

// classifyMCPResult keeps the canonical item-kind query separate from the
// remote tool's IsError bit. CanonicalItem.ToolResult's bool answers whether
// the item is a tool result; it does not classify the remote execution.
func classifyMCPResult(item canonical.CanonicalItem) (execution.Outcome, executionevidence.MCPFailure, bool) {
	result, ok := item.ToolResult()
	if !ok {
		return execution.Failed, executionevidence.FailureResultProjection, false
	}
	if result.IsError() {
		return execution.Failed, executionevidence.FailureToolError, true
	}
	return execution.Succeeded, "", true
}

func emitMCPSettlement(sink observation.ExecutionEvidenceSink, state exchangeState, phase callingMCPPhase, returned mcpToolReturned, outcome execution.Outcome, occurrence execution.Occurrence, failure executionevidence.MCPFailure) {
	if sink == nil || !phase.invocationID.Valid() {
		return
	}
	settled, err := executionevidence.NewMCPInvocationSettled(phase.invocationID, time.Now(), outcome, occurrence, failure)
	if err == nil {
		appendExecutionEvidence(sink, settled, phase.invocationID.ExchangeID().String())
	}
	_ = returned
}

func emitMCPInvocationStarted(sink observation.ExecutionEvidenceSink, resource executionevidence.MCPResource, invocationID execution.InvocationID, exchangeID string) {
	if sink == nil {
		return
	}
	started, err := executionevidence.NewMCPInvocationStarted(invocationID, time.Now(), resource)
	if err == nil {
		appendExecutionEvidence(sink, started, exchangeID)
	}
}

func continueAfterLocalResults(prepared continuity.ResolvedRequest, response completedProviderResponse, results []canonical.CanonicalItem) (continuity.ResolvedRequest, error) {
	return prepared.ContinueAfterLocalResult(response.continuation, results)
}
