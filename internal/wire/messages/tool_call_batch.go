package messages

import (
	"encoding/json"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func decodeMessagesToolCallBatch(raw json.RawMessage) (canonical.ToolCallBatchPolicy, error) {
	trimmed := strings.TrimSpace(string(raw)) // swobu:io-string source=boundary
	if trimmed == "" || trimmed == "null" {
		return canonical.ToolCallBatchPolicy{}, nil
	}
	var disabled bool
	if err := json.Unmarshal(raw, &disabled); err != nil {
		return canonical.ToolCallBatchPolicy{}, canonical.BadRequest("messages request disable_parallel_tool_use is invalid")
	}
	if disabled {
		return canonical.NewToolCallBatchPolicy(canonical.ToolCallBatchAtMostOne), nil
	}
	return canonical.ToolCallBatchPolicy{}, nil
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
