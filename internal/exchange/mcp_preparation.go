package exchange

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	trafficevidence "github.com/swobuforge/swobu/internal/domain/trafficevidence"
)

func beginMCPPreparation(s exchangeState, runner runtimeBundle) (reducerOutcome, error) {
	if s.draft == nil {
		return reducerOutcome{}, fmt.Errorf("exchange invariant: MCP preparation requires a continuity draft")
	}
	s.phase = preparingMCPPhase{}
	// MCP preparation is deliberately exchange-scoped and precedes provider
	// selection. One Run freezes the catalog and owns every MCP effect across
	// route candidates; providers receive only AttemptRequest's callable view.
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
	s.reusablePrefix = trafficevidence.UnknownReusablePrefix()
	if s.previousRequest != nil {
		comparison := canonical.CompareReusablePrefix(*s.previousRequest, next.Request())
		s.reusablePrefix = reusablePrefixEvidence(comparison, s.input.exchangeID)
	}
	s.mcp = prepared.run
	s.effectiveChanges = append(s.effectiveChanges, prepared.changes...)
	outcome, err := advanceProviderExecution(ctx, s, runner)
	return outcome, err
}

func reusablePrefixEvidence(comparison canonical.ReusablePrefixComparison, exchangeID string) trafficevidence.ReusablePrefixEvidence {
	if comparison.Preserved {
		return trafficevidence.PreservedReusablePrefix()
	}
	kind, occurrence := changedReusablePrefixOccurrence(comparison)
	evidence, err := trafficevidence.NewChangedReusablePrefix(kind)
	if err != nil {
		slog.Warn("reusable-prefix evidence skipped", "component", "exchange", "event", "reusable_prefix_evidence_invalid", "exchange_id", exchangeID, "error", err)
		return trafficevidence.UnknownReusablePrefix()
	}
	slog.Debug("reusable prefix changed", "component", "exchange", "event", "reusable_prefix_changed", "exchange_id", exchangeID, "kind", kind, "occurrence", occurrence.Key())
	return evidence
}

func changedReusablePrefixOccurrence(comparison canonical.ReusablePrefixComparison) (trafficevidence.ReusablePrefixChangeKind, canonical.Occurrence) {
	if comparison.InstructionChanged != nil {
		return trafficevidence.ReusablePrefixInstruction, *comparison.InstructionChanged
	}
	if comparison.ToolChanged != nil {
		return trafficevidence.ReusablePrefixTool, *comparison.ToolChanged
	}
	if comparison.InputChanged != nil {
		return trafficevidence.ReusablePrefixInput, *comparison.InputChanged
	}
	return "", canonical.Occurrence{}
}
