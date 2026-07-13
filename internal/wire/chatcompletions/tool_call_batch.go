package chatcompletions

import (
	"encoding/json"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func decodeChatCompletionsToolCallBatch(raw json.RawMessage) (canonical.ToolCallBatchPolicy, error) {
	trimmed := strings.TrimSpace(string(raw)) // swobu:io-string source=boundary
	if trimmed == "" || trimmed == "null" {
		return canonical.ToolCallBatchPolicy{}, nil
	}
	var enabled bool
	if err := json.Unmarshal(raw, &enabled); err != nil {
		return canonical.ToolCallBatchPolicy{}, canonical.BadRequest("chat completions request parallel_tool_calls is invalid")
	}
	if enabled {
		return canonical.ToolCallBatchPolicy{}, nil
	}
	return canonical.NewToolCallBatchPolicy(canonical.ToolCallBatchAtMostOne), nil
}

func encodeChatCompletionsToolCallBatch(payload map[string]any, policy canonical.ToolCallBatchPolicy, hasTools bool) error {
	if policy.IsZero() {
		return nil
	}
	if err := policy.Validate(); err != nil {
		return err
	}
	// When no tools are declared, at_most_one is inert and must stay omitted.
	if !hasTools {
		return nil
	}
	if policy.Mode == canonical.ToolCallBatchAtMostOne {
		payload["parallel_tool_calls"] = false
	}
	return nil
}
