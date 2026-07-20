package messages

import (
	"context"
	"strings"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// loweredMessagesInstructions makes the single-system-field approximation
// explicit instead of presenting canonical role/order loss as domain text.
type loweredMessagesInstructions struct {
	Text      string
	Exact     bool
	Decisions []compat.Decision
}

func flattenInstructionsForMessages(set canonical.InstructionSet) loweredMessagesInstructions {
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
	lowered := loweredMessagesInstructions{Text: out.String(), Exact: exact}
	if !exact && len(instructions) > 0 {
		lowered.Decisions = []compat.Decision{{Feature: compat.RequestInstructions, Outcome: compat.Approx, Subject: compat.Subject("messages.system")}}
	}
	return lowered
}

func commitMessagesInstructionDecisions(sink compat.Sink, exchangeID string, lowered loweredMessagesInstructions) error {
	if sink == nil || len(lowered.Decisions) == 0 {
		return nil
	}
	return sink.Commit(context.Background(), exchangeID, lowered.Decisions)
}
