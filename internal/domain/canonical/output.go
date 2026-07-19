package canonical

type OutputItemKind = ItemKind
type OutputItem = CanonicalItem

const (
	OutputItemText    = ItemKindText
	OutputItemToolUse = ItemKindToolUse
)

func NewTextOutputItem(itemID string, text string) CanonicalItem {
	item := NewTextItem(ItemAuthorAssistant, text)
	return itemWithID(item, itemID)
}

func NewToolUseOutputItem(itemID string, toolUseID string, name string, input ToolArguments) CanonicalItem {
	return NewToolUseItem(ItemAuthorAssistant, itemID, toolUseID, name, input)
}

func NewCustomToolUseOutputItem(itemID string, toolUseID string, name string, input ToolArguments) CanonicalItem {
	return NewCustomToolUseItem(ItemAuthorAssistant, itemID, toolUseID, name, input)
}

type CanonicalOutput interface {
	// SemanticKind reports which semantic family this successful canonical output represents.
	SemanticKind() SemanticKind
	// Response returns the canonical response identity and typed refinements.
	Response() ResponseRef
	// Model returns the backend model identity reported for this output when available.
	Model() string
	// FinishReason returns the provider-neutral terminal status for this output when available.
	FinishReason() string
	// Items returns the ordered semantic output items that client-family encoding must realize.
	Items() []CanonicalItem
	// Usage returns provider-neutral token and cache accounting when available.
	Usage() TokenUsage
	// CloneOutput returns a deep semantic copy suitable for cross-boundary handoff.
	CloneOutput() CanonicalOutput
}

// CanonicalOutputProjection is the fully materialized canonical success value in the canonical core.
// Streaming is modeled as ordered assembly of this object rather than as a separate semantic path.
type CanonicalOutputProjection struct {
	semanticKind SemanticKind
	response     ResponseRef
	model        string
	items        []CanonicalItem
	finishReason string
	usage        TokenUsage
}

func NewOutputWithUsage(semanticKind SemanticKind, response ResponseRef, model string, items []CanonicalItem, finishReason string, usage TokenUsage) CanonicalOutputProjection {
	return CanonicalOutputProjection{
		semanticKind: semanticKind,
		response:     response.Clone(),
		model:        model,
		items:        cloneCanonicalItems(items),
		finishReason: finishReason,
		usage:        usage,
	}
}

func NewConversationOutput(swobuResponseID SwobuResponseID, model string, items []CanonicalItem, finishReason string) CanonicalOutputProjection {
	return NewConversationOutputWithUsage(swobuResponseID, model, items, finishReason, NewUnknownTokenUsage())
}

func NewConversationOutputWithUsage(swobuResponseID SwobuResponseID, model string, items []CanonicalItem, finishReason string, usage TokenUsage) CanonicalOutputProjection {
	return newConversationOutputWithResponse(ResponseRef{SwobuID: swobuResponseID}, model, items, finishReason, usage)
}

func NewPromptOutput(swobuResponseID SwobuResponseID, model string, items []CanonicalItem, finishReason string) CanonicalOutputProjection {
	return NewPromptOutputWithUsage(swobuResponseID, model, items, finishReason, NewUnknownTokenUsage())
}

func NewPromptOutputWithUsage(swobuResponseID SwobuResponseID, model string, items []CanonicalItem, finishReason string, usage TokenUsage) CanonicalOutputProjection {
	return NewOutputWithUsage(SemanticKindPrompt, ResponseRef{SwobuID: swobuResponseID}, model, items, finishReason, usage)
}

func newConversationOutputWithResponse(response ResponseRef, model string, items []CanonicalItem, finishReason string, usage TokenUsage) CanonicalOutputProjection {
	return NewOutputWithUsage(SemanticKindConversation, response, model, items, finishReason, usage)
}

func (o CanonicalOutputProjection) SemanticKind() SemanticKind {
	return o.semanticKind
}

func (o CanonicalOutputProjection) Response() ResponseRef {
	return o.response.Clone()
}

func (o CanonicalOutputProjection) Model() string {
	return o.model
}

func (o CanonicalOutputProjection) FinishReason() string {
	return o.finishReason
}

func (o CanonicalOutputProjection) Items() []CanonicalItem {
	return cloneCanonicalItems(o.items)
}

func (o CanonicalOutputProjection) Usage() TokenUsage {
	return o.usage
}

func (o CanonicalOutputProjection) CloneOutput() CanonicalOutput {
	return o.CloneProjection()
}

// CloneProjection returns a deep copy of the concrete output projection
// without forcing callers through an interface assertion.
func (o CanonicalOutputProjection) CloneProjection() CanonicalOutputProjection {
	return NewOutputWithUsage(o.semanticKind, o.response, o.model, o.items, o.finishReason, o.usage)
}

func (o CanonicalOutputProjection) WithResponse(response ResponseRef) CanonicalOutputProjection {
	o.response = response.Clone()
	return o
}

func (o CanonicalOutputProjection) Text() string {
	out := ""
	for _, item := range o.items {
		if item.Kind() != ItemKindText {
			continue
		}
		text, ok := item.TextItem()
		if !ok {
			continue
		}
		out += text.Text
	}
	return out
}
