package responses

import (
	"encoding/json"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// responsesSuppliedFields records wire-level key occurrence only while the
// decoder constructs field-local canonical Specified values.
type responsesSuppliedFields struct {
	Model, Instructions, Tools, ToolPolicy, ToolCallBatch, OutputFormat bool
}

func decodeResponsesSuppliedFields(raw []byte) (responsesSuppliedFields, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return responsesSuppliedFields{}, canonical.BadRequest("responses request is invalid")
	}
	_, model := fields["model"]
	_, instructions := fields["instructions"]
	_, tools := fields["tools"]
	_, toolPolicy := fields["tool_choice"]
	_, toolCallBatch := fields["parallel_tool_calls"]
	_, outputFormat := fields["text"]
	return responsesSuppliedFields{
		Model: model, Instructions: instructions, Tools: tools,
		ToolPolicy: toolPolicy, ToolCallBatch: toolCallBatch, OutputFormat: outputFormat,
	}, nil
}
