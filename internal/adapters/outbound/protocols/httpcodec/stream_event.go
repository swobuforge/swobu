package httpcodec

import "github.com/swobuforge/swobu/internal/domain/canonical"

type StreamEventKind string

const (
	StreamEventStarted               StreamEventKind = "started"
	StreamEventItemStarted           StreamEventKind = "item_started"
	StreamEventTextDelta             StreamEventKind = "text_delta"
	StreamEventToolUseArgumentsDelta StreamEventKind = "tool_use_arguments_delta"
	StreamEventItemCompleted         StreamEventKind = "item_completed"
	StreamEventCompleted             StreamEventKind = "completed"
)

type StreamEvent struct {
	Kind StreamEventKind

	ResultID string
	Model    string

	ItemKind canonical.ItemKind
	ItemID   string

	TextDelta string

	ToolUseID      string
	Name           string
	ArgumentsDelta string

	FinishReason string
	Usage        canonical.TokenUsage
}
