package canonical

type OutputItemKind = ItemKind
type OutputItem = CanonicalItem

const (
	OutputItemText    = ItemKindText
	OutputItemToolUse = ItemKindToolUse
)

func NewTextOutputItem(itemID string, text string) CanonicalItem {
	item := NewTextItem(ItemAuthorAssistant, text)
	item.ItemID = itemID
	return item
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
	// ResultID returns the continuity-critical provider body identity when available.
	ResultID() string
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
	resultID     string
	model        string
	items        []CanonicalItem
	finishReason string
	usage        TokenUsage
}

func NewOutputWithUsage(semanticKind SemanticKind, resultID string, model string, items []CanonicalItem, finishReason string, usage TokenUsage) CanonicalOutputProjection {
	return CanonicalOutputProjection{
		semanticKind: semanticKind,
		resultID:     resultID,
		model:        model,
		items:        cloneCanonicalItems(items),
		finishReason: finishReason,
		usage:        usage,
	}
}

func NewConversationOutput(resultID string, model string, items []CanonicalItem, finishReason string) CanonicalOutputProjection {
	return NewConversationOutputWithUsage(resultID, model, items, finishReason, NewUnknownTokenUsage())
}

func NewConversationOutputWithUsage(resultID string, model string, items []CanonicalItem, finishReason string, usage TokenUsage) CanonicalOutputProjection {
	return NewOutputWithUsage(SemanticKindConversation, resultID, model, items, finishReason, usage)
}

func NewPromptOutput(resultID string, model string, items []CanonicalItem, finishReason string) CanonicalOutputProjection {
	return NewPromptOutputWithUsage(resultID, model, items, finishReason, NewUnknownTokenUsage())
}

func NewPromptOutputWithUsage(resultID string, model string, items []CanonicalItem, finishReason string, usage TokenUsage) CanonicalOutputProjection {
	return NewOutputWithUsage(SemanticKindPrompt, resultID, model, items, finishReason, usage)
}

func (o CanonicalOutputProjection) SemanticKind() SemanticKind {
	return o.semanticKind
}

func (o CanonicalOutputProjection) ResultID() string {
	return o.resultID
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
	return NewOutputWithUsage(o.semanticKind, o.resultID, o.model, o.items, o.finishReason, o.usage)
}

func (o CanonicalOutputProjection) WithResultID(id string) CanonicalOutputProjection {
	o.resultID = id
	return o
}

func (o CanonicalOutputProjection) Text() string {
	out := ""
	for _, item := range o.items {
		if item.Kind != ItemKindText {
			continue
		}
		out += item.Text
	}
	return out
}
