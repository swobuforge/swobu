package responses

import (
	"encoding/json"
	"strings"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func decodeResponsesToolCallBatch(raw json.RawMessage) (canonical.ToolCallBatchPolicy, error) {
	trimmed := strings.TrimSpace(string(raw)) // swobu:io-string source=boundary
	if trimmed == "" || trimmed == "null" {
		return canonical.ToolCallBatchPolicy{}, nil
	}
	var enabled bool
	if err := json.Unmarshal(raw, &enabled); err != nil {
		return canonical.ToolCallBatchPolicy{}, canonical.BadRequest("responses request parallel_tool_calls is invalid")
	}
	if enabled {
		return canonical.ToolCallBatchPolicy{}, nil
	}
	return canonical.NewToolCallBatchPolicy(canonical.ToolCallBatchAtMostOne), nil
}

func encodeResponsesToolCallBatch(payload map[string]any, policy canonical.ToolCallBatchPolicy, hasTools bool, rejectsFalse func() bool, changeLog *[]compat.Change) error {
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
		if rejectsFalse != nil && rejectsFalse() {
			if changeLog != nil {
				*changeLog = compat.AppendUnique(*changeLog, compat.NewOmission(canonical.RequestToolCallBatch, canonical.Occurrence{}))
			}
		} else {
			payload["parallel_tool_calls"] = false
		}
	}
	return nil
}
