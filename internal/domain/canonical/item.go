package canonical

import (
	"fmt"
	"strings"
)

// ItemKind identifies the populated branch of one CanonicalItem. Kind is
// derived from the private branch and is never independently mutable.
type ItemKind string

const (
	ItemKindMessage             ItemKind = "message"
	ItemKindToolDeclarations    ItemKind = "tool_declarations"
	ItemKindToolCall            ItemKind = "tool_call"
	ItemKindToolResult          ItemKind = "tool_result"
	ItemKindToolDiscoveryResult ItemKind = "tool_discovery_result"
	ItemKindReasoning           ItemKind = "reasoning"
)

// ContextScope controls whether one model-visible context occurrence survives
// the boundary after the current request.
type ContextScope uint8

const (
	ContextScopeHistory ContextScope = iota
	ContextScopeRequest
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
	message             *MessageItem
	toolDeclarations    *ToolDeclarationsItem
	toolCall            *ToolCallItem
	toolResult          *ToolResultItem
	toolDiscoveryResult *ToolDiscoveryResultItem
	reasoning           *ReasoningItem
}

// MessageItem owns one preserved wire message boundary and its ordered
// content-part sequence.
type MessageItem struct {
	role    MessageRole
	content []MessagePart
	scope   ContextScope
}

// ToolDeclarationsItem is one ordered contribution to the model-visible tool
// environment. Its scope belongs to this occurrence, not to the ToolSet.
type ToolDeclarationsItem struct {
	tools     ToolSet
	scope     ContextScope
	responses ResponsesToolRefinements
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

// ResponsesWebSearchRefinement preserves the exact Responses item identity of a
// web_search_call when the dialect supplied one. It is distinct from ToolCallID:
// correlation pairs a call with its result across the canonical graph, while the
// refinement is the provider-owned presentation id that must round-trip verbatim
// or be omitted entirely. Its absence is meaningful — a dialect that replayed a
// completed search with no id (notably Codex under store:false) must re-encode
// with no id, never with a synthetic correlation token minted into item.id.
type ResponsesWebSearchRefinement struct {
	itemID ResponsesItemID
}

// NewResponsesWebSearchRefinement validates one exact Responses item id as a
// web-search refinement. The zero ResponsesWebSearchRefinement means "no id was
// preserved" and is the only way to represent an omitted id.
func NewResponsesWebSearchRefinement(itemID ResponsesItemID) (ResponsesWebSearchRefinement, error) {
	if itemID.IsZero() {
		return ResponsesWebSearchRefinement{}, fmt.Errorf("responses web-search refinement requires a non-empty item id")
	}
	return ResponsesWebSearchRefinement{itemID: itemID}, nil
}

// ItemID returns the preserved Responses item identity.
func (r ResponsesWebSearchRefinement) ItemID() ResponsesItemID { return r.itemID }

// Clone returns an independent copy. The wrapped id is immutable.
func (r ResponsesWebSearchRefinement) Clone() ResponsesWebSearchRefinement { return r }

// ToolCallItem is one ordered tool invocation. CallID correlates a later result
// while Tool is sufficient to interpret and re-encode this historical call.
type ToolCallItem struct {
	callID              ToolCallID
	tool                ToolKey
	input               ToolInput
	discoveryExecutor   DiscoveryExecutor
	responsesNullCallID bool
	responsesWebSearch  *ResponsesWebSearchRefinement
}

// ToolResultItem is one ordered correlated result with ordered content.
type ToolResultItem struct {
	callID    ToolCallID
	content   *ToolContentResult
	webSearch *WebSearchResult
}

// ToolDiscoveryResultItem correlates one discovery call with the declarations
// it loaded. Those declarations become available only after this item.
type ToolDiscoveryResultItem struct {
	callID              ToolCallID
	tools               ToolSet
	executor            DiscoveryExecutor
	responses           ResponsesToolRefinements
	responsesNullCallID bool
}

// ResponsesToolRefinements owns Responses-only visibility facts for one
// declaration occurrence. Keys must name declarations inside that occurrence;
// permanent callable contracts remain protocol-neutral.
type ResponsesToolRefinements struct {
	deferred map[ToolKey]struct{}
}

// NewResponsesToolRefinements validates one occurrence-local deferred set.
func NewResponsesToolRefinements(tools ToolSet, deferred []ToolKey) (ResponsesToolRefinements, error) {
	known := callableToolKeys(tools)
	refinement := ResponsesToolRefinements{deferred: make(map[ToolKey]struct{}, len(deferred))}
	for _, key := range deferred {
		if key.IsZero() || (key.Kind() != ToolKindFunction && key.Kind() != ToolKindCustom) {
			return ResponsesToolRefinements{}, fmt.Errorf("Responses deferred tool refinement requires a callable key")
		}
		if _, ok := known[key]; !ok {
			return ResponsesToolRefinements{}, fmt.Errorf("Responses deferred tool refinement references an undeclared tool")
		}
		refinement.deferred[key.Clone()] = struct{}{}
	}
	return refinement, nil
}

func callableToolKeys(tools ToolSet) map[ToolKey]struct{} {
	known := make(map[ToolKey]struct{})
	var observe func([]ToolDeclaration)
	observe = func(declarations []ToolDeclaration) {
		for _, declaration := range declarations {
			if declaration.Kind() == ToolKindFunction || declaration.Kind() == ToolKindCustom {
				known[declaration.Key()] = struct{}{}
			}
			if namespace, ok := declaration.Namespace(); ok {
				observe(namespace.Tools())
			}
		}
	}
	observe(tools.Declarations())
	return known
}

func (r ResponsesToolRefinements) Deferred(key ToolKey) bool {
	_, ok := r.deferred[key]
	return ok
}

func (r ResponsesToolRefinements) DeferredKeys() []ToolKey {
	keys := make([]ToolKey, 0, len(r.deferred))
	for key := range r.deferred {
		keys = append(keys, key.Clone())
	}
	return keys
}

func (r ResponsesToolRefinements) Clone() ResponsesToolRefinements {
	cloned := ResponsesToolRefinements{deferred: make(map[ToolKey]struct{}, len(r.deferred))}
	for key := range r.deferred {
		cloned.deferred[key.Clone()] = struct{}{}
	}
	return cloned
}

// ToolContentResult is the ordinary caller-resolved function/custom result
// branch retained by NewToolResultItem.
type ToolContentResult struct {
	parts   []ToolResultPart
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

// ToolInput is the closed object, raw-text, or web-search input grammar for a
// tool call.
type ToolInput struct {
	object    *JSONObject
	text      *string
	webSearch *WebSearchCall
}

// NewMessageItem validates and constructs one message without coalescing or
// sorting its content parts.
func NewMessageItem(role MessageRole, content []MessagePart) (CanonicalItem, error) {
	return NewScopedMessageItem(role, content, ContextScopeHistory)
}

// NewScopedMessageItem constructs one message occurrence. Request scope is
// valid only for system/developer directives.
func NewScopedMessageItem(role MessageRole, content []MessagePart, scope ContextScope) (CanonicalItem, error) {
	if !validMessageRole(role) {
		return CanonicalItem{}, fmt.Errorf("canonical message role %q is invalid", role)
	}
	if !validContextScope(scope) {
		return CanonicalItem{}, fmt.Errorf("canonical message context scope is invalid")
	}
	if scope == ContextScopeRequest && role != MessageRoleSystem && role != MessageRoleDeveloper {
		return CanonicalItem{}, fmt.Errorf("canonical request-scoped messages require system or developer role")
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
	message := MessageItem{role: role, content: cloned, scope: scope}
	return CanonicalItem{message: &message}, nil
}

// NewToolDeclarationsItem constructs one ordered declaration contribution.
func NewToolDeclarationsItem(tools ToolSet, scope ContextScope) (CanonicalItem, error) {
	return NewToolDeclarationsItemWithResponses(tools, scope, ResponsesToolRefinements{})
}

// NewToolDeclarationsItemWithResponses constructs a declaration contribution
// with occurrence-local Responses refinements.
func NewToolDeclarationsItemWithResponses(tools ToolSet, scope ContextScope, responses ResponsesToolRefinements) (CanonicalItem, error) {
	if !validContextScope(scope) {
		return CanonicalItem{}, fmt.Errorf("canonical tool declaration context scope is invalid")
	}
	validated, err := NewResponsesToolRefinements(tools, responses.DeferredKeys())
	if err != nil {
		return CanonicalItem{}, err
	}
	item := ToolDeclarationsItem{tools: tools.Clone(), scope: scope, responses: validated}
	return CanonicalItem{toolDeclarations: &item}, nil
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
	case ToolKindDiscovery:
		return CanonicalItem{}, fmt.Errorf("canonical tool discovery call requires explicit execution ownership")
	case ToolKindCustom:
		if _, ok := input.Text(); !ok {
			return CanonicalItem{}, fmt.Errorf("canonical custom tool call requires text input")
		}
	case ToolKindWebSearch:
		if _, ok := input.WebSearch(); !ok {
			return CanonicalItem{}, fmt.Errorf("canonical web-search call requires web-search input")
		}
	default:
		return CanonicalItem{}, fmt.Errorf("canonical tool kind %q is not callable", tool.Kind())
	}
	call := ToolCallItem{callID: callID, tool: tool.Clone(), input: input.Clone()}
	return CanonicalItem{toolCall: &call}, nil
}

// NewToolCallItemWithResponsesWebSearch constructs one web-search call that also
// preserves the exact Responses item id the dialect supplied. The refinement is
// the provider-owned presentation identity; callID remains the canonical
// correlation token that pairs this call with its result. Pass a nil refinement
// when the dialect omitted the id (e.g. a Codex replay under store:false), in
// which case re-encode emits no item.id rather than minting callID into it.
func NewToolCallItemWithResponsesWebSearch(callID ToolCallID, tool ToolKey, input ToolInput, refinement *ResponsesWebSearchRefinement) (CanonicalItem, error) {
	item, err := NewToolCallItem(callID, tool, input)
	if err != nil {
		return CanonicalItem{}, err
	}
	call := item.toolCall
	call.responsesWebSearch = refinement
	return CanonicalItem{toolCall: call}, nil
}

// NewToolDiscoveryCallItem constructs one discovery call with the execution
// owner needed for replay, projection, and result ownership.
func NewToolDiscoveryCallItem(callID ToolCallID, input ToolInput, executor DiscoveryExecutor) (CanonicalItem, error) {
	return NewToolDiscoveryCallItemWithResponses(callID, input, executor, false)
}

// NewToolDiscoveryCallItemWithResponses preserves whether a hosted Responses
// lifecycle omitted its wire call ID while portable matching uses callID.
func NewToolDiscoveryCallItemWithResponses(callID ToolCallID, input ToolInput, executor DiscoveryExecutor, wireCallIDNull bool) (CanonicalItem, error) {
	if callID.IsZero() || (executor != DiscoveryExecutorClient && executor != DiscoveryExecutorProvider) {
		return CanonicalItem{}, fmt.Errorf("canonical tool discovery call is invalid")
	}
	if wireCallIDNull && executor != DiscoveryExecutorProvider {
		return CanonicalItem{}, fmt.Errorf("only provider-executed Responses discovery may omit its wire call id")
	}
	if _, ok := input.Object(); !ok {
		return CanonicalItem{}, fmt.Errorf("canonical tool discovery call requires object input")
	}
	call := ToolCallItem{
		callID: callID, tool: ToolDiscoveryKey(), input: input.Clone(),
		discoveryExecutor: executor, responsesNullCallID: wireCallIDNull,
	}
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
	contentResult := ToolContentResult{parts: cloned, isError: isError}
	result := ToolResultItem{callID: callID, content: &contentResult}
	return CanonicalItem{toolResult: &result}, nil
}

// NewToolDiscoveryResultItem constructs one correlated declaration-loading
// result. The loaded declarations are historical facts at this position.
func NewToolDiscoveryResultItem(callID ToolCallID, tools ToolSet, executor DiscoveryExecutor) (CanonicalItem, error) {
	return NewToolDiscoveryResultItemWithResponses(callID, tools, executor, ResponsesToolRefinements{})
}

// NewToolDiscoveryResultItemWithResponses constructs one loaded declaration
// occurrence with its exact Responses visibility refinements.
func NewToolDiscoveryResultItemWithResponses(callID ToolCallID, tools ToolSet, executor DiscoveryExecutor, responses ResponsesToolRefinements) (CanonicalItem, error) {
	return NewToolDiscoveryResultItemWithResponsesWireID(callID, tools, executor, responses, false)
}

// NewToolDiscoveryResultItemWithResponsesWireID preserves an absent hosted
// Responses wire ID without weakening portable correlation.
func NewToolDiscoveryResultItemWithResponsesWireID(callID ToolCallID, tools ToolSet, executor DiscoveryExecutor, responses ResponsesToolRefinements, wireCallIDNull bool) (CanonicalItem, error) {
	if callID.IsZero() || (executor != DiscoveryExecutorClient && executor != DiscoveryExecutorProvider) {
		return CanonicalItem{}, fmt.Errorf("canonical tool discovery result requires a call id")
	}
	if wireCallIDNull && executor != DiscoveryExecutorProvider {
		return CanonicalItem{}, fmt.Errorf("only provider-executed Responses discovery may omit its wire call id")
	}
	validated, err := NewResponsesToolRefinements(tools, responses.DeferredKeys())
	if err != nil {
		return CanonicalItem{}, err
	}
	result := ToolDiscoveryResultItem{callID: callID, tools: tools.Clone(), executor: executor, responses: validated, responsesNullCallID: wireCallIDNull}
	return CanonicalItem{toolDiscoveryResult: &result}, nil
}

// NewWebSearchResultItem constructs one exchange-resolved search result.
func NewWebSearchResultItem(callID ToolCallID, result WebSearchResult) (CanonicalItem, error) {
	if callID.IsZero() {
		return CanonicalItem{}, fmt.Errorf("canonical web-search result requires a call id")
	}
	if !result.valid() {
		return CanonicalItem{}, fmt.Errorf("canonical web-search result is invalid")
	}
	cloned := result.Clone()
	item := ToolResultItem{callID: callID, webSearch: &cloned}
	return CanonicalItem{toolResult: &item}, nil
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

// NewWebSearchToolInput validates and constructs a typed provider-observed
// search input. Validation errors remain visible to the wire boundary that
// supplied the action instead of becoming an unrelated zero-input failure.
func NewWebSearchToolInput(call WebSearchCall) (ToolInput, error) {
	if err := call.Validate(); err != nil {
		return ToolInput{}, err
	}
	cloned := call.Clone()
	return ToolInput{webSearch: &cloned}, nil
}

// Kind returns the populated branch kind, or the zero kind for an invalid
// package-local zero value.
func (i CanonicalItem) Kind() ItemKind {
	switch {
	case i.message != nil && i.toolDeclarations == nil && i.toolCall == nil && i.toolResult == nil && i.toolDiscoveryResult == nil && i.reasoning == nil:
		return ItemKindMessage
	case i.message == nil && i.toolDeclarations != nil && i.toolCall == nil && i.toolResult == nil && i.toolDiscoveryResult == nil && i.reasoning == nil:
		return ItemKindToolDeclarations
	case i.message == nil && i.toolDeclarations == nil && i.toolCall != nil && i.toolResult == nil && i.toolDiscoveryResult == nil && i.reasoning == nil:
		return ItemKindToolCall
	case i.message == nil && i.toolDeclarations == nil && i.toolCall == nil && i.toolResult != nil && i.toolDiscoveryResult == nil && i.reasoning == nil:
		return ItemKindToolResult
	case i.message == nil && i.toolDeclarations == nil && i.toolCall == nil && i.toolResult == nil && i.toolDiscoveryResult != nil && i.reasoning == nil:
		return ItemKindToolDiscoveryResult
	case i.message == nil && i.toolDeclarations == nil && i.toolCall == nil && i.toolResult == nil && i.toolDiscoveryResult == nil && i.reasoning != nil:
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
		default:
			return ""
		}
	case ItemKindToolCall:
		return TurnOwnerAssistant
	case ItemKindToolResult:
		return TurnOwnerUser
	case ItemKindToolDiscoveryResult:
		result, _ := i.ToolDiscoveryResult()
		if result.Executor() == DiscoveryExecutorProvider {
			return TurnOwnerAssistant
		}
		return TurnOwnerUser
	case ItemKindReasoning:
		return TurnOwnerAssistant
	}
	return ""
}

// ToolDeclarations returns an independent declaration occurrence.
func (i CanonicalItem) ToolDeclarations() (ToolDeclarationsItem, bool) {
	if i.Kind() != ItemKindToolDeclarations {
		return ToolDeclarationsItem{}, false
	}
	return i.toolDeclarations.Clone(), true
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

// ToolDiscoveryResult returns an independent discovery-result occurrence.
func (i CanonicalItem) ToolDiscoveryResult() (ToolDiscoveryResultItem, bool) {
	if i.Kind() != ItemKindToolDiscoveryResult {
		return ToolDiscoveryResultItem{}, false
	}
	return i.toolDiscoveryResult.Clone(), true
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
	if i.toolDeclarations != nil {
		value := i.toolDeclarations.Clone()
		cloned.toolDeclarations = &value
	}
	if i.toolCall != nil {
		value := i.toolCall.Clone()
		cloned.toolCall = &value
	}
	if i.toolResult != nil {
		value := i.toolResult.Clone()
		cloned.toolResult = &value
	}
	if i.toolDiscoveryResult != nil {
		value := i.toolDiscoveryResult.Clone()
		cloned.toolDiscoveryResult = &value
	}
	if i.reasoning != nil {
		value := i.reasoning.Clone()
		cloned.reasoning = &value
	}
	return cloned
}

func (m MessageItem) Role() MessageRole      { return m.role }
func (m MessageItem) Content() []MessagePart { return cloneMessageParts(m.content) }
func (m MessageItem) Scope() ContextScope    { return m.scope }
func (m MessageItem) Clone() MessageItem {
	return MessageItem{role: m.role, content: cloneMessageParts(m.content), scope: m.scope}
}

func (d ToolDeclarationsItem) Tools() ToolSet                      { return d.tools.Clone() }
func (d ToolDeclarationsItem) Scope() ContextScope                 { return d.scope }
func (d ToolDeclarationsItem) Responses() ResponsesToolRefinements { return d.responses.Clone() }
func (d ToolDeclarationsItem) Clone() ToolDeclarationsItem {
	return ToolDeclarationsItem{tools: d.tools.Clone(), scope: d.scope, responses: d.responses.Clone()}
}

func (c ToolCallItem) CallID() ToolCallID { return c.callID }
func (c ToolCallItem) Tool() ToolKey      { return c.tool.Clone() }
func (c ToolCallItem) Input() ToolInput   { return c.input.Clone() }
func (c ToolCallItem) DiscoveryExecutor() (DiscoveryExecutor, bool) {
	if c.tool.Kind() != ToolKindDiscovery ||
		(c.discoveryExecutor != DiscoveryExecutorClient && c.discoveryExecutor != DiscoveryExecutorProvider) {
		return 0, false
	}
	return c.discoveryExecutor, true
}
func (c ToolCallItem) ResponsesCallIDNull() bool { return c.responsesNullCallID }

// ResponsesWebSearch returns the exact Responses item id preserved for this
// web-search call, or false when the dialect omitted one. It is the only legal
// source of a web_search_call item.id on re-encode; callID must never be used.
func (c ToolCallItem) ResponsesWebSearch() (ResponsesWebSearchRefinement, bool) {
	if c.responsesWebSearch == nil {
		return ResponsesWebSearchRefinement{}, false
	}
	return c.responsesWebSearch.Clone(), true
}
func (c ToolCallItem) Clone() ToolCallItem {
	cloned := ToolCallItem{
		callID: c.callID, tool: c.tool.Clone(), input: c.input.Clone(),
		discoveryExecutor:   c.discoveryExecutor,
		responsesNullCallID: c.responsesNullCallID,
	}
	if c.responsesWebSearch != nil {
		ref := c.responsesWebSearch.Clone()
		cloned.responsesWebSearch = &ref
	}
	return cloned
}
func (r ToolResultItem) CallID() ToolCallID { return r.callID }
func (r ToolResultItem) Content() []ToolResultPart {
	if r.content == nil || r.webSearch != nil {
		return nil
	}
	return cloneToolResultParts(r.content.parts)
}
func (r ToolResultItem) IsError() bool {
	return r.content != nil && r.webSearch == nil && r.content.isError
}

// WebSearch returns the typed search-result branch when it is the sole branch.
func (r ToolResultItem) WebSearch() (WebSearchResult, bool) {
	if r.webSearch == nil || r.content != nil {
		return WebSearchResult{}, false
	}
	return r.webSearch.Clone(), true
}
func (r ToolResultItem) Clone() ToolResultItem {
	if r.content != nil && r.webSearch == nil {
		content := ToolContentResult{parts: cloneToolResultParts(r.content.parts), isError: r.content.isError}
		return ToolResultItem{callID: r.callID, content: &content}
	}
	if search, ok := r.WebSearch(); ok {
		return ToolResultItem{callID: r.callID, webSearch: &search}
	}
	return ToolResultItem{}
}
func (r ToolDiscoveryResultItem) CallID() ToolCallID                  { return r.callID }
func (r ToolDiscoveryResultItem) Tools() ToolSet                      { return r.tools.Clone() }
func (r ToolDiscoveryResultItem) Executor() DiscoveryExecutor         { return r.executor }
func (r ToolDiscoveryResultItem) Responses() ResponsesToolRefinements { return r.responses.Clone() }
func (r ToolDiscoveryResultItem) ResponsesCallIDNull() bool           { return r.responsesNullCallID }
func (r ToolDiscoveryResultItem) Clone() ToolDiscoveryResultItem {
	return ToolDiscoveryResultItem{callID: r.callID, tools: r.tools.Clone(), executor: r.executor, responses: r.responses.Clone(), responsesNullCallID: r.responsesNullCallID}
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
	if i.object == nil || i.text != nil || i.webSearch != nil {
		return JSONObject{}, false
	}
	return i.object.Clone(), true
}

// Text returns raw text input when populated.
func (i ToolInput) Text() (string, bool) {
	if i.text == nil || i.object != nil || i.webSearch != nil {
		return "", false
	}
	return *i.text, true
}

// WebSearch returns the typed search call when populated.
func (i ToolInput) WebSearch() (WebSearchCall, bool) {
	if i.webSearch == nil || i.object != nil || i.text != nil {
		return WebSearchCall{}, false
	}
	return i.webSearch.Clone(), true
}

// Clone returns an independent tool input.
func (i ToolInput) Clone() ToolInput {
	if i.object != nil && i.text == nil {
		return NewJSONObjectToolInput(*i.object)
	}
	if i.text != nil && i.object == nil && i.webSearch == nil {
		return NewTextToolInput(*i.text)
	}
	if i.webSearch != nil && i.object == nil && i.text == nil {
		cloned := i.webSearch.Clone()
		return ToolInput{webSearch: &cloned}
	}
	return ToolInput{}
}

func (i ToolInput) valid() bool {
	branches := 0
	if i.object != nil {
		branches++
	}
	if i.text != nil {
		branches++
	}
	if i.webSearch != nil {
		branches++
	}
	return branches == 1
}

func validMessageRole(role MessageRole) bool {
	switch role {
	case MessageRoleUser, MessageRoleAssistant, MessageRoleSystem, MessageRoleDeveloper:
		return true
	default:
		return false
	}
}

func validContextScope(scope ContextScope) bool {
	return scope == ContextScopeHistory || scope == ContextScopeRequest
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
