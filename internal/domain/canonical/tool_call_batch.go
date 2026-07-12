package canonical

import (
	"encoding/json"
	"strings"
)

// ToolCallBatchMode names the request-side tool batching lowerings currently
// supported by the canonical request grammar.
type ToolCallBatchMode string

const (
	ToolCallBatchUnspecified ToolCallBatchMode = ""
	ToolCallBatchAtMostOne   ToolCallBatchMode = "at_most_one"
)

// ToolCallBatchPolicy carries the canonical request intent for tool-call
// batching. It stays separate from tool choice so adapters can lower or reject
// batching independently from tool selection.
type ToolCallBatchPolicy struct {
	Mode ToolCallBatchMode
}

func NewToolCallBatchPolicy(mode ToolCallBatchMode) ToolCallBatchPolicy {
	return ToolCallBatchPolicy{Mode: mode}
}

func (p ToolCallBatchPolicy) Clone() ToolCallBatchPolicy {
	return ToolCallBatchPolicy{Mode: p.Mode}
}

func (p ToolCallBatchPolicy) IsZero() bool {
	return strings.TrimSpace(string(p.Mode)) == ""
}

func (p ToolCallBatchPolicy) Validate() error {
	switch p.Mode {
	case ToolCallBatchUnspecified, ToolCallBatchAtMostOne:
		return nil
	default:
		return BadRequest("tool call batch policy mode is invalid")
	}
}

type requestToolCallBatchMetadataDTO struct {
	Mode string `json:"mode"`
}

func decodeToolCallBatchMetadata(raw string) (ToolCallBatchPolicy, error) {
	trimmed := strings.TrimSpace(raw) // swobu:io-string source=domain
	if trimmed == "" || trimmed == "null" {
		return ToolCallBatchPolicy{}, nil
	}
	var dto requestToolCallBatchMetadataDTO
	if err := json.Unmarshal([]byte(trimmed), &dto); err != nil {
		return ToolCallBatchPolicy{}, BadRequest("canonical request tool call batch policy is invalid")
	}
	mode := ToolCallBatchMode(strings.TrimSpace(dto.Mode))
	switch mode {
	case ToolCallBatchUnspecified:
		return ToolCallBatchPolicy{}, BadRequest("canonical request tool call batch policy mode is required")
	case ToolCallBatchAtMostOne:
		return ToolCallBatchPolicy{Mode: mode}, nil
	default:
		return ToolCallBatchPolicy{}, BadRequest("canonical request tool call batch policy mode is invalid")
	}
}
