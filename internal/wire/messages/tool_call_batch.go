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

func encodeMessagesToolCallBatch(payload map[string]any, policy canonical.ToolCallBatchPolicy, hasTools bool) error {
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
		payload["disable_parallel_tool_use"] = true
	}
	return nil
}
