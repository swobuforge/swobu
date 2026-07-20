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

func flattenInstructionsForResponses(set canonical.InstructionSet) loweredResponsesInstructions {
	instructions := set.Instructions()
	var out strings.Builder
	exact := len(instructions) <= 1
	for index, instruction := range instructions {
		if index > 0 {
			out.WriteString("\n\n")
		}
		out.WriteString(instruction.Text())
		if instruction.Role() != canonical.MessageRoleSystem {
			exact = false
		}
	}
	lowered := loweredResponsesInstructions{Text: out.String(), Exact: exact}
	if !exact && len(instructions) > 0 {
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
