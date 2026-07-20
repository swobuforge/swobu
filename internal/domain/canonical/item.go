package canonical

import (
	"fmt"
	"strings"
)

// ItemKind identifies the populated branch of one CanonicalItem. Kind is
// derived from the private branch and is never independently mutable.
type ItemKind string

const (
	ItemKindMessage    ItemKind = "message"
	ItemKindToolCall   ItemKind = "tool_call"
	ItemKindToolResult ItemKind = "tool_result"
	ItemKindReasoning  ItemKind = "reasoning"
)

// TurnOwner identifies the conversational side that owns an ordered item.
// Instructions remain directives rather than conversational turns.
type TurnOwner string

const (
	TurnOwnerUser      TurnOwner = "user"
	TurnOwnerAssistant TurnOwner = "assistant"
)

// MessageRole identifies the semantic role of one message.
type MessageRole string

const (
	MessageRoleUser      MessageRole = "user"
	MessageRoleAssistant MessageRole = "assistant"
	MessageRoleSystem    MessageRole = "system"
	MessageRoleDeveloper MessageRole = "developer"
)

// CanonicalItem is one exclusive branch in the ordered exchange grammar.
// Private concrete branches make contradictory item combinations
// unrepresentable outside this package.
type CanonicalItem struct {
	message    *MessageItem
	toolCall   *ToolCallItem
	toolResult *ToolResultItem
	reasoning  *ReasoningItem
}

// MessageItem owns one preserved wire message boundary and its ordered
// content-part sequence.
type MessageItem struct {
	role    MessageRole
	content []MessagePart
}

// ToolCallID is the canonical correlation identity shared by one tool call and
// its later result. It is not a durable item identity or stream-routing key.
type ToolCallID struct {
	value string
}

// NewToolCallID validates one call-correlation value where it first becomes
// canonical.
func NewToolCallID(raw string) (ToolCallID, error) {
	if raw == "" || strings.TrimSpace(raw) != raw { // swobu:io-string source=domain
		return ToolCallID{}, fmt.Errorf("canonical tool call id is empty")
	}
	return ToolCallID{value: raw}, nil
}

// String returns the canonical correlation token.
func (id ToolCallID) String() string { return id.value }

// IsZero reports whether no validated token is present.
func (id ToolCallID) IsZero() bool { return id.value == "" }

// ToolCallItem is one ordered tool invocation. CallID correlates a later result
// while Tool is sufficient to interpret and re-encode this historical call.
type ToolCallItem struct {
	callID ToolCallID
	tool   ToolKey
	input  ToolInput
}

// ToolResultItem is one ordered correlated result with ordered content.
type ToolResultItem struct {
	callID  ToolCallID
	content []ToolResultPart
	isError bool
}

type ReasoningPartKind string

const (
	ReasoningPartSummary ReasoningPartKind = "summary"
	ReasoningPartTrace   ReasoningPartKind = "trace"
)

// ReasoningPart is one non-empty readable artifact returned through a provider
// reasoning channel. Summary and reasoning-channel text remain distinct.
type ReasoningPart struct {
	kind ReasoningPartKind
	text string
}

// ReasoningItem is one assistant-owned ordered reasoning artifact.
type ReasoningItem struct {
	parts  []ReasoningPart
	opaque OpaqueThinking
}

// ToolInput is the closed object-or-raw-text input grammar for a tool call.
type ToolInput struct {
	object *JSONObject
	text   *string
}

// NewMessageItem validates and constructs one message without coalescing or
// sorting its content parts.
func NewMessageItem(role MessageRole, content []MessagePart) (CanonicalItem, error) {
	if !validMessageRole(role) {
		return CanonicalItem{}, fmt.Errorf("canonical message role %q is invalid", role)
	}
	if role != MessageRoleUser {
		for _, part := range content {
			if part.Kind() == PartKindImage {
				return CanonicalItem{}, fmt.Errorf("canonical image messages require user role")
			}
		}
	}
	cloned, err := cloneValidatedMessageParts(content)
	if err != nil {
		return CanonicalItem{}, err
	}
	message := MessageItem{role: role, content: cloned}
	return CanonicalItem{message: &message}, nil
}

// NewToolCallItem validates and constructs one correlated semantic tool call.
func NewToolCallItem(callID ToolCallID, tool ToolKey, input ToolInput) (CanonicalItem, error) {
	if callID.IsZero() {
		return CanonicalItem{}, fmt.Errorf("canonical tool call requires a call id")
	}
	if tool.IsZero() {
		return CanonicalItem{}, fmt.Errorf("canonical tool call requires a valid tool key")
	}
	if !input.valid() {
		return CanonicalItem{}, fmt.Errorf("canonical tool call requires exactly one input branch")
	}
	switch tool.Kind() {
	case ToolKindFunction:
		if _, ok := input.Object(); !ok {
			return CanonicalItem{}, fmt.Errorf("canonical function call requires object input")
		}
	case ToolKindCustom:
		if _, ok := input.Text(); !ok {
			return CanonicalItem{}, fmt.Errorf("canonical custom tool call requires text input")
		}
	default:
		return CanonicalItem{}, fmt.Errorf("canonical tool kind %q is not callable", tool.Kind())
	}
	call := ToolCallItem{callID: callID, tool: tool.Clone(), input: input.Clone()}
	return CanonicalItem{toolCall: &call}, nil
}

// NewToolResultItem validates and constructs one correlated result.
func NewToolResultItem(callID ToolCallID, content []ToolResultPart, isError bool) (CanonicalItem, error) {
	if callID.IsZero() {
		return CanonicalItem{}, fmt.Errorf("canonical tool result requires a call id")
	}
	cloned, err := cloneValidatedToolResultParts(content)
	if err != nil {
		return CanonicalItem{}, err
	}
	result := ToolResultItem{callID: callID, content: cloned, isError: isError}
	return CanonicalItem{toolResult: &result}, nil
}

func NewReasoningPart(kind ReasoningPartKind, text string) (ReasoningPart, error) {
	if kind != ReasoningPartSummary && kind != ReasoningPartTrace {
		return ReasoningPart{}, fmt.Errorf("canonical reasoning part kind %q is invalid", kind)
	}
	if text == "" {
		return ReasoningPart{}, fmt.Errorf("canonical reasoning part text is empty")
	}
	return ReasoningPart{kind: kind, text: text}, nil
}

// NewReasoningItem constructs one reasoning branch. Opaque-only reasoning is
// legal when one complete typed replay branch is present.
func NewReasoningItem(parts []ReasoningPart, opaque OpaqueThinking) (CanonicalItem, error) {
	cloned, err := cloneValidatedReasoningParts(parts)
	if err != nil {
		return CanonicalItem{}, err
	}
	if err := opaque.validate(); err != nil {
		return CanonicalItem{}, err
	}
	if len(cloned) == 0 && opaque.IsZero() {
		return CanonicalItem{}, fmt.Errorf("canonical reasoning item requires readable parts or opaque thinking")
	}
	return CanonicalItem{reasoning: &ReasoningItem{parts: cloned, opaque: opaque.Clone()}}, nil
}

// NewJSONObjectToolInput constructs an object-semantic tool input.
func NewJSONObjectToolInput(object JSONObject) ToolInput {
	cloned := object.Clone()
	return ToolInput{object: &cloned}
}

// NewTextToolInput constructs a raw-text tool input, including empty text.
func NewTextToolInput(text string) ToolInput {
	cloned := text
	return ToolInput{text: &cloned}
}

// Kind returns the populated branch kind, or the zero kind for an invalid
// package-local zero value.
func (i CanonicalItem) Kind() ItemKind {
	switch {
	case i.message != nil && i.toolCall == nil && i.toolResult == nil && i.reasoning == nil:
		return ItemKindMessage
	case i.message == nil && i.toolCall != nil && i.toolResult == nil && i.reasoning == nil:
		return ItemKindToolCall
	case i.message == nil && i.toolCall == nil && i.toolResult != nil && i.reasoning == nil:
		return ItemKindToolResult
	case i.message == nil && i.toolCall == nil && i.toolResult == nil && i.reasoning != nil:
		return ItemKindReasoning
	default:
		return ""
	}
}

// Owner returns the turn owner used by protocols that group ordered items
// into role-bearing messages.
func (i CanonicalItem) Owner() TurnOwner {
	switch i.Kind() {
	case ItemKindMessage:
		message, _ := i.Message()
		switch message.Role() {
		case MessageRoleUser:
			return TurnOwnerUser
		case MessageRoleAssistant:
			return TurnOwnerAssistant
		}
	case ItemKindToolCall:
		return TurnOwnerAssistant
	case ItemKindToolResult:
		return TurnOwnerUser
	case ItemKindReasoning:
		return TurnOwnerAssistant
	}
	return ""
}

// Message returns an independent message value when this is a message item.
func (i CanonicalItem) Message() (MessageItem, bool) {
	if i.Kind() != ItemKindMessage {
		return MessageItem{}, false
	}
	return i.message.Clone(), true
}

// ToolCall returns an independent tool-call value when populated.
func (i CanonicalItem) ToolCall() (ToolCallItem, bool) {
	if i.Kind() != ItemKindToolCall {
		return ToolCallItem{}, false
	}
	return i.toolCall.Clone(), true
}

// ToolResult returns an independent tool-result value when populated.
func (i CanonicalItem) ToolResult() (ToolResultItem, bool) {
	if i.Kind() != ItemKindToolResult {
		return ToolResultItem{}, false
	}
	return i.toolResult.Clone(), true
}

func (i CanonicalItem) Reasoning() (ReasoningItem, bool) {
	if i.Kind() != ItemKindReasoning {
		return ReasoningItem{}, false
	}
	return i.reasoning.Clone(), true
}

// Clone returns a deeply independent item.
func (i CanonicalItem) Clone() CanonicalItem {
	cloned := CanonicalItem{}
	if i.message != nil {
		value := i.message.Clone()
		cloned.message = &value
	}
	if i.toolCall != nil {
		value := i.toolCall.Clone()
		cloned.toolCall = &value
	}
	if i.toolResult != nil {
		value := i.toolResult.Clone()
		cloned.toolResult = &value
	}
	if i.reasoning != nil {
		value := i.reasoning.Clone()
		cloned.reasoning = &value
	}
	return cloned
}

func (m MessageItem) Role() MessageRole      { return m.role }
func (m MessageItem) Content() []MessagePart { return cloneMessageParts(m.content) }
func (m MessageItem) Clone() MessageItem {
	return MessageItem{role: m.role, content: cloneMessageParts(m.content)}
}

func (c ToolCallItem) CallID() ToolCallID { return c.callID }
func (c ToolCallItem) Tool() ToolKey      { return c.tool.Clone() }
func (c ToolCallItem) Input() ToolInput   { return c.input.Clone() }
func (c ToolCallItem) Clone() ToolCallItem {
	return ToolCallItem{callID: c.callID, tool: c.tool.Clone(), input: c.input.Clone()}
}
func (r ToolResultItem) CallID() ToolCallID        { return r.callID }
func (r ToolResultItem) Content() []ToolResultPart { return cloneToolResultParts(r.content) }
func (r ToolResultItem) IsError() bool             { return r.isError }
func (r ToolResultItem) Clone() ToolResultItem {
	return ToolResultItem{callID: r.callID, content: cloneToolResultParts(r.content), isError: r.isError}
}
func (p ReasoningPart) Kind() ReasoningPartKind { return p.kind }
func (p ReasoningPart) Text() string            { return p.text }
func (r ReasoningItem) Parts() []ReasoningPart  { return cloneReasoningParts(r.parts) }
func (r ReasoningItem) Opaque() OpaqueThinking  { return r.opaque.Clone() }
func (r ReasoningItem) Clone() ReasoningItem {
	return ReasoningItem{parts: cloneReasoningParts(r.parts), opaque: r.opaque.Clone()}
}

// Object returns an independent object input when populated.
func (i ToolInput) Object() (JSONObject, bool) {
	if i.object == nil || i.text != nil {
		return JSONObject{}, false
	}
	return i.object.Clone(), true
}

// Text returns raw text input when populated.
func (i ToolInput) Text() (string, bool) {
	if i.text == nil || i.object != nil {
		return "", false
	}
	return *i.text, true
}

// Clone returns an independent tool input.
func (i ToolInput) Clone() ToolInput {
	if i.object != nil && i.text == nil {
		return NewJSONObjectToolInput(*i.object)
	}
	if i.text != nil && i.object == nil {
		return NewTextToolInput(*i.text)
	}
	return ToolInput{}
}

func (i ToolInput) valid() bool {
	return (i.object == nil) != (i.text == nil)
}

func validMessageRole(role MessageRole) bool {
	switch role {
	case MessageRoleUser, MessageRoleAssistant, MessageRoleSystem, MessageRoleDeveloper:
		return true
	default:
		return false
	}
}

func cloneCanonicalItems(items []CanonicalItem) []CanonicalItem {
	if items == nil {
		return nil
	}
	cloned := make([]CanonicalItem, len(items))
	for index := range items {
		cloned[index] = items[index].Clone()
	}
	return cloned
}

func cloneValidatedReasoningParts(parts []ReasoningPart) ([]ReasoningPart, error) {
	cloned := make([]ReasoningPart, len(parts))
	for index, part := range parts {
		if part.kind != ReasoningPartSummary && part.kind != ReasoningPartTrace {
			return nil, fmt.Errorf("canonical reasoning part %d kind %q is invalid", index, part.kind)
		}
		if part.text == "" {
			return nil, fmt.Errorf("canonical reasoning part %d text is empty", index)
		}
		cloned[index] = part
	}
	return cloned, nil
}

func cloneReasoningParts(parts []ReasoningPart) []ReasoningPart {
	if parts == nil {
		return nil
	}
	return append([]ReasoningPart(nil), parts...)
}
