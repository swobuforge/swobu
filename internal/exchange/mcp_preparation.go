package exchange

import (
	"context"
	"fmt"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func beginMCPPreparation(s exchangeState, runner runtimeBundle) (reducerOutcome, error) {
	if s.draft == nil {
		return reducerOutcome{}, fmt.Errorf("exchange invariant: MCP preparation requires a session draft")
	}
	s.phase = preparingMCPPhase{}
	return reducerOutcome{
		nextState: s,
		command: prepareMCPCommand{
			full: s.draft.Current(), access: s.input.mcpAccess,
		},
	}, nil
}

func reducePreparingMCP(ctx context.Context, s exchangeState, event exchangeEvent, runner runtimeBundle) (reducerOutcome, error) {
	prepared, ok := event.(mcpPrepared)
	if !ok {
		return reducerOutcome{}, fmt.Errorf("exchange invariant: preparing MCP received %T", event)
	}
	if prepared.err != nil {
		s.phase = failedPhase{problem: prepared.err}
		return reducerOutcome{nextState: s}, nil
	}
	next, err := s.draft.Finalize(prepared.full)
	if err != nil {
		s.phase = failedPhase{problem: canonical.InternalError("MCP preparation changed resolved request layout: " + err.Error())}
		return reducerOutcome{nextState: s}, nil
	}
	s.draft = nil
	s.prepared = &next
	s.mcp = prepared.run
	s.effectiveChanges = append(s.effectiveChanges, prepared.changes...)
	outcome, err := advanceProviderExecution(ctx, s, runner)
	return outcome, err
}
