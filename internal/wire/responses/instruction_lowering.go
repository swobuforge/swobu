package responses

import (
	"strings"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

type loweredResponsesInstructions struct {
	Text    string
	Exact   bool
	Changes []compat.Change
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
		lowered.Changes = []compat.Change{compat.NewApproximation(canonical.RequestInstructions, canonical.Occurrence{})}
	}
	return lowered
}
