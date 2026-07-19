package canonical

import "fmt"

// ItemAuthor records who authored one canonical item so family-specific
// envelopes can be reconstructed without guessing from neighboring shapes.
type ItemAuthor string

const (
	ItemAuthorUser      ItemAuthor = "user"
	ItemAuthorAssistant ItemAuthor = "assistant"
	ItemAuthorTool      ItemAuthor = "tool"
)

// ItemKind is the shared semantic atom used by canonical requests, canonical
// outputs, and replay history.
type ItemKind string

const (
	ItemKindText       ItemKind = "text"
	ItemKindToolUse    ItemKind = "tool_use"
	ItemKindToolResult ItemKind = "tool_result"
)

// CanonicalItem is one ordered semantic unit in the canonical core.
// Requests, outputs, and persisted replay state all reuse this shape so
// history is not modeled as a second parallel object graph.
type CanonicalItem struct {
	author  ItemAuthor
	itemID  string
	payload itemPayload
}

// itemPayload is the sealed sum of exclusive canonical item arms.
type itemPayload interface {
	isItemPayload()
	clone() itemPayload
}

type TextItemPayload struct{ Text string }
type ToolUseItemPayload struct {
	ToolType string
	UseID    string
	Name     string
	Input    ToolArguments
}
type ToolResultItemPayload struct {
	UseID string
	Text  string
}

func (TextItemPayload) isItemPayload()       {}
func (ToolUseItemPayload) isItemPayload()    {}
func (ToolResultItemPayload) isItemPayload() {}

func (p TextItemPayload) clone() itemPayload { return p }
func (p ToolUseItemPayload) clone() itemPayload {
	return ToolUseItemPayload{ToolType: p.ToolType, UseID: p.UseID, Name: p.Name, Input: NewToolArgumentsObject(p.Input.RawObject())}
}
func (p ToolResultItemPayload) clone() itemPayload { return p }

func NewTextItem(author ItemAuthor, text string) CanonicalItem {
	return CanonicalItem{author: author, payload: TextItemPayload{Text: text}}
}

func NewToolUseItem(author ItemAuthor, itemID string, toolUseID string, name string, input ToolArguments) CanonicalItem {
	return CanonicalItem{author: author, itemID: itemID, payload: ToolUseItemPayload{
		ToolType: ToolTypeFunction, UseID: toolUseID, Name: name,
		Input: NewToolArgumentsObject(input.RawObject()),
	}}
}

func NewCustomToolUseItem(author ItemAuthor, itemID string, toolUseID string, name string, input ToolArguments) CanonicalItem {
	item := NewToolUseItem(author, itemID, toolUseID, name, input)
	payload := item.payload.(ToolUseItemPayload)
	payload.ToolType = ToolTypeCustom
	item.payload = payload
	return item
}

func NewToolResultItem(author ItemAuthor, toolUseID string, text string) CanonicalItem {
	return CanonicalItem{author: author, payload: ToolResultItemPayload{UseID: toolUseID, Text: text}}
}

func (i CanonicalItem) Clone() CanonicalItem {
	cloned := CanonicalItem{author: i.author, itemID: i.itemID}
	if i.payload != nil {
		cloned.payload = i.payload.clone()
	}
	return cloned
}

func (i CanonicalItem) Author() ItemAuthor { return i.author }
func (i CanonicalItem) ItemID() string     { return i.itemID }
func (i CanonicalItem) Kind() ItemKind {
	switch i.payload.(type) {
	case TextItemPayload:
		return ItemKindText
	case ToolUseItemPayload:
		return ItemKindToolUse
	case ToolResultItemPayload:
		return ItemKindToolResult
	default:
		return ""
	}
}
func (i CanonicalItem) TextItem() (TextItemPayload, bool) {
	payload, ok := i.payload.(TextItemPayload)
	return payload, ok
}
func (i CanonicalItem) ToolUse() (ToolUseItemPayload, bool) {
	payload, ok := i.payload.(ToolUseItemPayload)
	if !ok {
		return ToolUseItemPayload{}, false
	}
	payload.Input = NewToolArgumentsObject(payload.Input.RawObject())
	return payload, true
}
func (i CanonicalItem) ToolResult() (ToolResultItemPayload, bool) {
	payload, ok := i.payload.(ToolResultItemPayload)
	return payload, ok
}

func itemWithAuthor(i CanonicalItem, author ItemAuthor) CanonicalItem { i.author = author; return i }
func itemWithID(i CanonicalItem, itemID string) CanonicalItem         { i.itemID = itemID; return i }
func appendTextItemDelta(i CanonicalItem, delta string) (CanonicalItem, error) {
	payload, ok := i.payload.(TextItemPayload)
	if !ok {
		return CanonicalItem{}, fmt.Errorf("text delta requires text item, got %q", i.Kind())
	}
	payload.Text += delta
	i.payload = payload
	return i, nil
}
func appendToolResultTextDelta(i CanonicalItem, delta string) (CanonicalItem, error) {
	payload, ok := i.payload.(ToolResultItemPayload)
	if !ok {
		return CanonicalItem{}, fmt.Errorf("tool-result text delta requires tool-result item, got %q", i.Kind())
	}
	payload.Text += delta
	i.payload = payload
	return i, nil
}
func withToolUseInput(i CanonicalItem, input ToolArguments) (CanonicalItem, error) {
	payload, ok := i.payload.(ToolUseItemPayload)
	if !ok {
		return CanonicalItem{}, fmt.Errorf("tool input requires tool-use item, got %q", i.Kind())
	}
	payload.Input = NewToolArgumentsObject(input.RawObject())
	i.payload = payload
	return i, nil
}
func withToolUseType(i CanonicalItem, toolType string) (CanonicalItem, error) {
	payload, ok := i.payload.(ToolUseItemPayload)
	if !ok {
		return CanonicalItem{}, fmt.Errorf("tool type requires tool-use item, got %q", i.Kind())
	}
	payload.ToolType = toolType
	i.payload = payload
	return i, nil
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
