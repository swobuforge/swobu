package zai

import (
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/wire/reasoningprojection"
)

type thinkingOptions struct {
	Type string `json:"type"`
}

// applyReasoning projects canonical current-turn reasoning into the static Z.AI
// Chat Completions dialect established by the committed live contract.
func applyReasoning(
	request canonical.CanonicalRequest,
	target protocolcodec.ReasoningTargetDialect,
	changes *[]compat.Change,
	exchangeID string,
) (map[string]any, error) {
	fields := make(map[string]any)
	compute, computeSpecified := request.Reasoning().ComputeField().Get()
	effort, effortSpecified := request.Controls().Effort.Get()

	if computeSpecified && compute.Kind() == canonical.ReasoningDisabled {
		if target.ProjectDisabled(changes) {
			fields["thinking"] = thinkingOptions{Type: "disabled"}
		}
		if effortSpecified {
			appendReasoningChange(changes, compat.NewOmission(canonical.RequestControlsEffort, canonical.Occurrence{}))
		}
		return fields, nil
	}

	if computeSpecified {
		fields["thinking"] = thinkingOptions{Type: "enabled"}
	}
	if effortSpecified {
		fields["reasoning_effort"] = string(target.ProjectEffort(effort, changes))
		if computeSpecified && compute.Kind() == canonical.ReasoningBudget {
			appendReasoningChange(changes, reasoningBudgetApproximation())
		}
		return fields, nil
	}
	if !computeSpecified || compute.Kind() != canonical.ReasoningBudget {
		return fields, nil
	}

	tokens, _ := compute.Tokens()
	fields["reasoning_effort"] = string(reasoningprojection.EffortFromReferenceReasoningBudget(tokens))
	appendReasoningChange(changes, reasoningBudgetApproximation())
	return fields, nil
}

func reasoningBudgetApproximation() compat.Change {
	return compat.NewApproximation(
		canonical.RequestReasoning,

		canonical.Occurrence{})

}

func appendReasoningChange(changes *[]compat.Change, change compat.Change) {
	if changes == nil {
		return
	}
	*changes = compat.AppendUnique(*changes, change)
}
