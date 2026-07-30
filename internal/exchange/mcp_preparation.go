package exchange

import (
	"fmt"

	"github.com/swobuforge/swobu/internal/session"
)

func beginMCPPreparation(s exchangeState, runner runtimeBundle) (reducerOutcome, error) {
	if s.prepared == nil {
		return reducerOutcome{}, fmt.Errorf("exchange invariant: MCP preparation requires a resolved request")
	}
	s.phase = preparingMCPPhase{}
	return reducerOutcome{
		nextState: s,
		command: prepareMCPCommand{
			full: s.prepared.Full.Clone(), access: s.input.mcpAccess,
		},
	}, nil
}

func reducePreparingMCP(s exchangeState, event exchangeEvent, runner runtimeBundle) (reducerOutcome, error) {
	prepared, ok := event.(mcpPrepared)
	if !ok {
		return reducerOutcome{}, fmt.Errorf("exchange invariant: preparing MCP received %T", event)
	}
	if prepared.err != nil {
		s.phase = failedPhase{problem: prepared.err}
		return reducerOutcome{nextState: s}, nil
	}
	s.prepared = &session.ResolvedRequest{
		Full: prepared.full, Delta: s.prepared.Delta.Clone(),
		ResolvedMedia: s.prepared.ResolvedMedia.Clone(),
	}
	s.mcp = prepared.run
	s.effectiveChanges = append(s.effectiveChanges, prepared.changes...)
	outcome, err := advanceProviderExecution(s, runner)
	return outcome, err
}
