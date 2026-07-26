package responses

import (
	"context"
	"strings"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

type loweredResponsesInstructions struct {
	Text      string
	Exact     bool
	Decisions []compat.Decision
}

func flattenInstructionsForResponses(items []canonical.CanonicalItem) loweredResponsesInstructions {
	var out strings.Builder
	count := 0
	exact := true
	for _, item := range items {
		instruction, ok := item.Message()
		if !ok || instruction.Scope() != canonical.ContextScopeRequest {
			continue
		}
		if count > 0 {
			out.WriteString("\n\n")
		}
		for _, part := range instruction.Content() {
			if text, ok := part.Text(); ok {
				out.WriteString(text.Text())
			}
		}
		if instruction.Role() != canonical.MessageRoleSystem {
			exact = false
		}
		count++
	}
	exact = exact && count <= 1
	lowered := loweredResponsesInstructions{Text: out.String(), Exact: exact}
	if !exact && count > 0 {
		lowered.Decisions = []compat.Decision{{Feature: compat.RequestInstructions, Outcome: compat.Approx, Subject: compat.Subject("responses.instructions")}}
	}
	return lowered
}

func commitResponsesInstructionDecisions(sink compat.Sink, exchangeID string, lowered loweredResponsesInstructions) error {
	if sink == nil || len(lowered.Decisions) == 0 {
		return nil
	}
	return sink.Commit(context.Background(), exchangeID, lowered.Decisions)
}
