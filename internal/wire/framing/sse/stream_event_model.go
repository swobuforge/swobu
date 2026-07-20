package sse

import "github.com/swobuforge/swobu/internal/domain/canonical"

type StreamEventKind string

const (
	StreamEventStarted               StreamEventKind = "started"
	StreamEventItemStarted           StreamEventKind = "item_started"
	StreamEventContentStarted        StreamEventKind = "content_started"
	StreamEventTextDelta             StreamEventKind = "text_delta"
	StreamEventToolUseArgumentsDelta StreamEventKind = "tool_use_arguments_delta"
	StreamEventItemCompleted         StreamEventKind = "item_completed"
	StreamEventCompleted             StreamEventKind = "completed"
	StreamEventFailed                StreamEventKind = "failed"
)

type StreamEvent struct {
	Kind StreamEventKind

	ResultID string
	Model    string

	ItemKind      canonical.ItemKind
	ItemID        string
	CompletedItem *canonical.CanonicalItem
	// ItemOrdinal and PartOrdinal preserve canonical stream topology through
	// the family-specific client encoders.
	ItemOrdinal uint32
	PartOrdinal uint32
	// Exactly one part kind is populated for content_started. Keeping the
	// parent-specific types distinct prevents reasoning from becoming message
	// text inside family-specific encoders.
	PartKind canonical.PartKind

	TextDelta string

	ToolUseID      string
	Name           string
	ToolType       string
	ArgumentsDelta string

	FinishReason string
	Usage        canonical.TokenUsage

	ErrorCode    string
	ErrorMessage string
}
