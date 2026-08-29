package messages

import (
	"strings"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// loweredMessagesInstructions makes the single-system-field approximation
// explicit instead of presenting canonical role/order loss as domain text.
type loweredMessagesInstructions struct {
	Text    string
	Exact   bool
	Changes []compat.Change
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
		lowered.Changes = []compat.Change{compat.NewApproximation(canonical.RequestInstructions, canonical.Occurrence{})}
	}
	return lowered
}
