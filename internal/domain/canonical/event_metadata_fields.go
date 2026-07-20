package canonical

import (
	"fmt"
	"time"
)

type EnvelopeID string

type EventKind string

const (
	EventEnvelopeStart    EventKind = "envelope.start"
	EventEnvelopeEnd      EventKind = "envelope.end"
	EventItemStart        EventKind = "item.start"
	EventContentStart     EventKind = "content.start"
	EventTextDelta        EventKind = "text.delta"
	EventArgsDelta        EventKind = "args.delta"
	EventItemCompleted    EventKind = "item.completed"
	EventResponseIdentity EventKind = "response.identity"
	EventUsage            EventKind = "usage"
	EventFinish           EventKind = "finish"
	EventError            EventKind = "error"
)

type EnvelopeKind string

const (
	EnvResponse EnvelopeKind = "response"
)

type EventSource string

const (
	EventSourceClient EventSource = "client"
	EventSourceVendor EventSource = "vendor"
	EventSourceCore   EventSource = "core"
)

type EventMetadataFields struct {
	// Source marks the emitting edge (client/vendor/core) for provenance.
	Source    EventSource
	Vendor    string
	Model     string
	Protocol  string
	Transport string

	// Synthetic marks events generated from a buffered snapshot rather than
	// directly decoded from a native stream.
	Synthetic bool
	// Buffered marks events that crossed a projection/materialization boundary.
	Buffered bool
	// Degraded marks behavior downgrades (for example stream-shaped batch).
	Degraded bool

	// Native identifiers are references only; canonical envelope IDs stay
	// primary and stable throughout the exchange.
	NativeID    string
	NativeIndex *int
}

// Event is the canonical envelope event used as internal wire truth.
type Event struct {
	ExchangeID string
	Seq        int64
	Time       time.Time

	Kind     EventKind
	EnvID    EnvelopeID
	ParentID EnvelopeID

	Payload any
	Meta    EventMetadataFields
}

type EnvelopeStatus string

const (
	EnvelopeStatusCompleted EnvelopeStatus = "completed"
	EnvelopeStatusError     EnvelopeStatus = "error"
)

type EnvelopeStartPayload struct {
	Kind  EnvelopeKind
	Model string
}

// ResponseIdentityPayload carries canonical response identity independently
// of provider wire aliases and stream-correlation envelope IDs.
type ResponseIdentityPayload struct {
	Response ResponseRef
}

type ItemStartPayload struct {
	message  *MessageStart
	toolCall *ToolCallStart
}

// MessageStart carries the author required before content deltas are projected.
type MessageStart struct {
	Author MessageRole
}

// ToolCallStart carries declaration identity and invocation correlation before
// the first argument delta.
type ToolCallStart struct {
	CallID ToolCallID
	Tool   ToolKey
}

// NewMessageStart constructs the exclusive message-start branch.
func NewMessageStart(author MessageRole) (ItemStartPayload, error) {
	if !validMessageRole(author) {
		return ItemStartPayload{}, fmt.Errorf("canonical message start role %q is invalid", author)
	}
	start := MessageStart{Author: author}
	return ItemStartPayload{message: &start}, nil
}

// NewToolCallStart constructs the exclusive tool-call-start branch.
func NewToolCallStart(callID ToolCallID, tool ToolKey) (ItemStartPayload, error) {
	if callID.IsZero() || tool.IsZero() {
		return ItemStartPayload{}, fmt.Errorf("canonical tool-call start requires call and tool identities")
	}
	start := ToolCallStart{CallID: callID, Tool: tool.Clone()}
	return ItemStartPayload{toolCall: &start}, nil
}

func messageStartFromValidatedItem(author MessageRole) ItemStartPayload {
	start := MessageStart{Author: author}
	return ItemStartPayload{message: &start}
}

func toolCallStartFromValidatedItem(callID ToolCallID, tool ToolKey) ItemStartPayload {
	start := ToolCallStart{CallID: callID, Tool: tool.Clone()}
	return ItemStartPayload{toolCall: &start}
}

// Kind derives the item kind from the exclusive start branch.
func (p ItemStartPayload) Kind() ItemKind {
	if p.message != nil && p.toolCall == nil {
		return ItemKindMessage
	}
	if p.message == nil && p.toolCall != nil && !p.toolCall.CallID.IsZero() && !p.toolCall.Tool.IsZero() {
		return ItemKindToolCall
	}
	return ""
}

// Message returns the message start when populated.
func (p ItemStartPayload) Message() (MessageStart, bool) {
	if p.Kind() != ItemKindMessage {
		return MessageStart{}, false
	}
	return *p.message, true
}

// ToolCall returns an independent tool-call start when populated.
func (p ItemStartPayload) ToolCall() (ToolCallStart, bool) {
	if p.Kind() != ItemKindToolCall {
		return ToolCallStart{}, false
	}
	return ToolCallStart{CallID: p.toolCall.CallID, Tool: p.toolCall.Tool.Clone()}, true
}

// ItemPosition is the explicit stream-local coordinate for progressive item
// evidence. Part is meaningful only for content-scoped events.
type ItemPosition struct {
	Item uint32
	Part uint32
}

// ItemEvent correlates every item-scoped payload by an ephemeral position that
// never enters CanonicalItem.
type ItemEvent struct {
	Position ItemPosition
	Payload  ItemEventPayload
}

// ItemEventPayload is the package-sealed progressive item payload set.
type ItemEventPayload interface {
	isItemEventPayload()
}

// ContentStartPayload identifies one progressive content part.
type ContentStartPayload struct {
	Kind PartKind
}

// NewMessageContentStart constructs a content start for one message part.
func NewMessageContentStart(kind PartKind) ContentStartPayload {
	return ContentStartPayload{Kind: kind}
}

func messageContentStartFromValidatedPart(kind PartKind) ContentStartPayload {
	return ContentStartPayload{Kind: kind}
}

// ItemCompletedPayload is the lossless canonical checkpoint used by buffered
// projection and checkpoint commit.
type ItemCompletedPayload struct {
	Item CanonicalItem
}

func (ItemStartPayload) isItemEventPayload()     {}
func (ContentStartPayload) isItemEventPayload()  {}
func (TextDeltaPayload) isItemEventPayload()     {}
func (ArgsDeltaPayload) isItemEventPayload()     {}
func (ItemCompletedPayload) isItemEventPayload() {}

type EnvelopeEndPayload struct {
	Kind   EnvelopeKind
	Status EnvelopeStatus
}

type TextDeltaPayload struct {
	Text string
}

type ArgsDeltaPayload struct {
	Args string
}

type UsagePayload struct {
	Usage TokenUsage
}

type FinishPayload struct {
	Reason string
}

type ErrorPayload struct {
	Code      string
	Message   string
	Retryable bool
}
