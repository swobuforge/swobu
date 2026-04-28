package compatibility

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

func NewToolUseOutputItem(itemID string, toolUseID string, name string, input map[string]any) CanonicalItem {
	return NewToolUseItem(ItemAuthorAssistant, itemID, toolUseID, name, input)
}

type CanonicalOutput interface {
	// SemanticKind reports which semantic family this successful canonical output represents.
	SemanticKind() SemanticKind
	// ResultID returns the continuity-critical provider result identity when available.
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

// CanonicalOutputValue is the fully materialized canonical success value in the compatibility core.
// Streaming is modeled as ordered assembly of this object rather than as a separate semantic path.
type CanonicalOutputValue struct {
	semanticKind SemanticKind
	resultID     string
	model        string
	items        []CanonicalItem
	finishReason string
	usage        TokenUsage
}

func NewOutput(semanticKind SemanticKind, resultID string, model string, items []CanonicalItem, finishReason string) CanonicalOutputValue {
	return NewOutputWithUsage(semanticKind, resultID, model, items, finishReason, NewUnknownTokenUsage())
}

func NewOutputWithUsage(semanticKind SemanticKind, resultID string, model string, items []CanonicalItem, finishReason string, usage TokenUsage) CanonicalOutputValue {
	return CanonicalOutputValue{
		semanticKind: semanticKind,
		resultID:     resultID,
		model:        model,
		items:        cloneCanonicalItems(items),
		finishReason: finishReason,
		usage:        usage,
	}
}

func NewConversationOutput(resultID string, model string, items []CanonicalItem, finishReason string) CanonicalOutputValue {
	return NewConversationOutputWithUsage(resultID, model, items, finishReason, NewUnknownTokenUsage())
}

func NewConversationOutputWithUsage(resultID string, model string, items []CanonicalItem, finishReason string, usage TokenUsage) CanonicalOutputValue {
	return NewOutputWithUsage(SemanticKindConversation, resultID, model, items, finishReason, usage)
}

func NewPromptOutput(resultID string, model string, items []CanonicalItem, finishReason string) CanonicalOutputValue {
	return NewPromptOutputWithUsage(resultID, model, items, finishReason, NewUnknownTokenUsage())
}

func NewPromptOutputWithUsage(resultID string, model string, items []CanonicalItem, finishReason string, usage TokenUsage) CanonicalOutputValue {
	return NewOutputWithUsage(SemanticKindPrompt, resultID, model, items, finishReason, usage)
}

func (o CanonicalOutputValue) SemanticKind() SemanticKind {
	return o.semanticKind
}

func (o CanonicalOutputValue) ResultID() string {
	return o.resultID
}

func (o CanonicalOutputValue) Model() string {
	return o.model
}

func (o CanonicalOutputValue) FinishReason() string {
	return o.finishReason
}

func (o CanonicalOutputValue) Items() []CanonicalItem {
	return cloneCanonicalItems(o.items)
}

func (o CanonicalOutputValue) Usage() TokenUsage {
	return o.usage
}

func (o CanonicalOutputValue) CloneOutput() CanonicalOutput {
	return NewOutputWithUsage(o.semanticKind, o.resultID, o.model, o.items, o.finishReason, o.usage)
}

func (o CanonicalOutputValue) Text() string {
	out := ""
	for _, item := range o.items {
		if item.Kind != ItemKindText {
			continue
		}
		out += item.Text
	}
	return out
}

// CloneCanonicalOutput protects cross-boundary output handoff from accidental mutation.
func CloneCanonicalOutput(output CanonicalOutput) CanonicalOutput {
	if output == nil {
		return nil
	}
	return output.CloneOutput()
}
