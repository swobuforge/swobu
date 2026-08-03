package zai

import (
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/wire/chatcompletions"
	"github.com/swobuforge/swobu/internal/wire/reasoningprojection"
)

type thinkingOptions struct {
	Type string `json:"type"`
}

// applyReasoning projects canonical current-turn reasoning into the static Z.AI
// Chat Completions dialect established by the committed live contract.
func applyReasoning(
	document *chatcompletions.ProviderRequestDocument,
	request canonical.CanonicalRequest,
	changes *[]compat.Change,
) {
	compute, computeSpecified := request.Reasoning().ComputeField().Get()
	effort, effortSpecified := request.Controls().Effort.Get()

	if computeSpecified && compute.Kind() == canonical.ReasoningDisabled {
		document.Payload["thinking"] = thinkingOptions{Type: "disabled"}
		if effortSpecified {
			appendReasoningChange(changes, compat.NewOmission(canonical.RequestControlsEffort, canonical.Occurrence{}))
		}
		return
	}

	if computeSpecified {
		document.Payload["thinking"] = thinkingOptions{Type: "enabled"}
	}
	if effortSpecified {
		document.Payload["reasoning_effort"] = string(effort)
		if computeSpecified && compute.Kind() == canonical.ReasoningBudget {
			appendReasoningChange(changes, reasoningBudgetApproximation())
		}
		return
	}
	if !computeSpecified || compute.Kind() != canonical.ReasoningBudget {
		return
	}

	tokens, _ := compute.Tokens()
	document.Payload["reasoning_effort"] = string(reasoningprojection.EffortFromReferenceReasoningBudget(tokens))
	appendReasoningChange(changes, reasoningBudgetApproximation())
}

func reasoningBudgetApproximation() compat.Change {
	return compat.NewApproximation(
		canonical.RequestReasoning,
		canonical.RequestControlsEffort,
		canonical.Occurrence{},
	)
}

func appendReasoningChange(changes *[]compat.Change, change compat.Change) {
	if changes == nil {
		return
	}
	*changes = compat.AppendUnique(*changes, change)
}
