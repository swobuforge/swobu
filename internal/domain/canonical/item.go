package canonical

// ItemAuthor records who authored one canonical item so family-specific
// envelopes can be reconstructed without guessing from neighboring shapes.
type ItemAuthor string

const (
	ItemAuthorUser      ItemAuthor = "user"
	ItemAuthorAssistant ItemAuthor = "assistant"
	ItemAuthorTool      ItemAuthor = "tool"
)

// ItemKind is the shared semantic atom used by canonical requests, canonical
// outputs, and continuity history.
type ItemKind string

const (
	ItemKindText       ItemKind = "text"
	ItemKindToolUse    ItemKind = "tool_use"
	ItemKindToolResult ItemKind = "tool_result"
)

// CanonicalItem is one ordered semantic unit in the compatibility core.
// Requests, outputs, and persisted continuation state all reuse this shape so
// history is not modeled as a second parallel object graph.
type CanonicalItem struct {
	Author    ItemAuthor
	Kind      ItemKind
	ItemID    string
	Text      string
	ToolUseID string
	Name      string
	Input     map[string]any
}

func NewTextItem(author ItemAuthor, text string) CanonicalItem {
	return CanonicalItem{
		Author: author,
		Kind:   ItemKindText,
		Text:   text,
	}
}

func NewToolUseItem(author ItemAuthor, itemID string, toolUseID string, name string, input map[string]any) CanonicalItem {
	return CanonicalItem{
		Author:    author,
		Kind:      ItemKindToolUse,
		ItemID:    itemID,
		ToolUseID: toolUseID,
		Name:      name,
		Input:     cloneStringAnyMap(input),
	}
}

func NewToolResultItem(author ItemAuthor, toolUseID string, text string) CanonicalItem {
	return CanonicalItem{
		Author:    author,
		Kind:      ItemKindToolResult,
		ToolUseID: toolUseID,
		Text:      text,
	}
}

func (i CanonicalItem) Clone() CanonicalItem {
	return CanonicalItem{
		Author:    i.Author,
		Kind:      i.Kind,
		ItemID:    i.ItemID,
		Text:      i.Text,
		ToolUseID: i.ToolUseID,
		Name:      i.Name,
		Input:     cloneStringAnyMap(i.Input),
	}
}

func cloneCanonicalItems(items []CanonicalItem) []CanonicalItem {
	if items == nil {
		return nil
	}
	cloned := make([]CanonicalItem, len(items))
	for i := range items {
		cloned[i] = items[i].Clone()
	}
	return cloned
}
