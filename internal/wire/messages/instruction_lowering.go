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

func flattenInstructionsForMessages(items []canonical.CanonicalItem) loweredMessagesInstructions {
	var out strings.Builder
	count := 0
	exact := true
	for _, item := range items {
		instruction, ok := item.Message()
		if !ok || (instruction.Role() != canonical.MessageRoleSystem && instruction.Role() != canonical.MessageRoleDeveloper) {
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
	lowered := loweredMessagesInstructions{Text: out.String(), Exact: exact}
	if !exact && count > 0 {
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
