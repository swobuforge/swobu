package messages

import (
	"encoding/json"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func decodeMessagesToolCallBatch(toolChoiceRaw json.RawMessage, topLevelRaw json.RawMessage) (canonical.ToolCallBatchPolicy, bool, error) {
	var nestedSpecified bool
	var nestedDisabled bool
	if trimmedChoice := strings.TrimSpace(string(toolChoiceRaw)); trimmedChoice != "" && trimmedChoice != "null" && strings.HasPrefix(trimmedChoice, "{") {
		var rawFields map[string]json.RawMessage
		if err := json.Unmarshal(toolChoiceRaw, &rawFields); err == nil {
			if field, exists := rawFields["disable_parallel_tool_use"]; exists {
				fieldTrimmed := strings.TrimSpace(string(field))
				if fieldTrimmed != "" && fieldTrimmed != "null" {
					var flag bool
					if err := json.Unmarshal(field, &flag); err != nil {
						return canonical.ToolCallBatchPolicy{}, false, canonical.BadRequest("messages request tool_choice disable_parallel_tool_use is invalid")
					}
					nestedSpecified = true
					nestedDisabled = flag
				}
			}
		}
	}

	var topSpecified bool
	var topDisabled bool
	if trimmedTop := strings.TrimSpace(string(topLevelRaw)); trimmedTop != "" && trimmedTop != "null" {
		var flag bool
		if err := json.Unmarshal(topLevelRaw, &flag); err != nil {
			return canonical.ToolCallBatchPolicy{}, false, canonical.BadRequest("messages request disable_parallel_tool_use is invalid")
		}
		topSpecified = true
		topDisabled = flag
	}

	if nestedSpecified && topSpecified && nestedDisabled != topDisabled {
		return canonical.ToolCallBatchPolicy{}, false, canonical.BadRequest("messages request specifies conflicting disable_parallel_tool_use")
	}

	if (nestedSpecified && nestedDisabled) || (topSpecified && topDisabled) {
		return canonical.NewToolCallBatchPolicy(canonical.ToolCallBatchAtMostOne), true, nil
	}
	return canonical.ToolCallBatchPolicy{}, nestedSpecified || topSpecified, nil
}

func encodeMessagesToolCallBatch(toolChoice any, policy canonical.ToolCallBatchPolicy, hasTools bool) (any, error) {
	if policy.IsZero() {
		return toolChoice, nil
	}
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	// When no tools are declared, at_most_one is inert and must stay omitted.
	if !hasTools {
		return toolChoice, nil
	}
	if policy.Mode == canonical.ToolCallBatchAtMostOne {
		if toolChoice == nil {
			toolChoice = map[string]any{"type": "auto"}
		}
		payload, ok := toolChoice.(map[string]any)
		if !ok {
			return nil, canonical.InternalError("messages protocol tool_choice payload is invalid")
		}
		payload["disable_parallel_tool_use"] = true
		return payload, nil
	}
	return toolChoice, nil
}
