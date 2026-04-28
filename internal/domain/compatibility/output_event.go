package compatibility

import (
	"encoding/json"
	"fmt"
	"io"
)

type OutputEventKind string

const (
	OutputEventStarted               OutputEventKind = "started"
	OutputEventItemStarted           OutputEventKind = "item_started"
	OutputEventTextDelta             OutputEventKind = "text_delta"
	OutputEventToolUseArgumentsDelta OutputEventKind = "tool_use_arguments_delta"
	OutputEventItemCompleted         OutputEventKind = "item_completed"
	OutputEventCompleted             OutputEventKind = "completed"
)

// OutputEvent is one incremental semantic update toward a fully materialized canonical output.
// Delivery mode changes how the output is observed, not what the output means.
type OutputEvent struct {
	Kind           OutputEventKind
	ResultID       string
	Model          string
	Usage          TokenUsage
	ItemKind       ItemKind
	ItemID         string
	ToolUseID      string
	Name           string
	TextDelta      string
	ArgumentsDelta string
	FinishReason   string
}

// CanonicalOutputEventStream yields incremental semantic updates after provider decoding.
// Provider SSE frames must be normalized into this stream before they can flow
// back toward client-family encoding.
type CanonicalOutputEventStream interface {
	Next() (OutputEvent, error)
	Close() error
}

type SliceEventStreamCloser struct {
	events []OutputEvent
	index  int
}

func NewSliceEventStream(events []OutputEvent) *SliceEventStreamCloser {
	cloned := make([]OutputEvent, len(events))
	copy(cloned, events)
	return &SliceEventStreamCloser{events: cloned}
}

func (s *SliceEventStreamCloser) Next() (OutputEvent, error) {
	if s.index >= len(s.events) {
		return OutputEvent{}, io.EOF
	}
	event := s.events[s.index]
	s.index++
	return event, nil
}

func (s *SliceEventStreamCloser) Close() error {
	return nil
}

// OutputAssembler incrementally builds the same canonical output object used on
// the buffered path. It exists to keep buffered and streaming semantics unified.
type OutputAssembler struct {
	semanticKind SemanticKind
	resultID     string
	model        string
	finishReason string
	usage        TokenUsage
	items        []CanonicalItem
	indexByID    map[string]int
}

func NewOutputAssembler(semanticKind SemanticKind) *OutputAssembler {
	return &OutputAssembler{
		semanticKind: semanticKind,
		indexByID:    map[string]int{},
	}
}

// kinds while preserving one ordered semantic output model.
func (a *OutputAssembler) Apply(event OutputEvent) error {
	switch event.Kind {
	case OutputEventStarted:
		if event.ResultID != "" {
			a.resultID = event.ResultID
		}
		if event.Model != "" {
			a.model = event.Model
		}
		return nil
	case OutputEventItemStarted:
		if event.ItemID == "" {
			return fmt.Errorf("output item start is missing item id")
		}
		if _, exists := a.indexByID[event.ItemID]; exists {
			return fmt.Errorf("output item %q started twice", event.ItemID)
		}
		var item CanonicalItem
		switch event.ItemKind {
		case ItemKindText:
			item = NewTextOutputItem(event.ItemID, "")
		case ItemKindToolUse:
			item = NewToolUseOutputItem(event.ItemID, event.ToolUseID, event.Name, nil)
		default:
			return fmt.Errorf("output item kind %q is unsupported", event.ItemKind)
		}
		a.indexByID[event.ItemID] = len(a.items)
		a.items = append(a.items, item)
		return nil
	case OutputEventTextDelta:
		item, err := a.item(event.ItemID, ItemKindText)
		if err != nil {
			return err
		}
		item.Text += event.TextDelta
		return nil
	case OutputEventToolUseArgumentsDelta:
		item, err := a.item(event.ItemID, ItemKindToolUse)
		if err != nil {
			return err
		}
		if item.Input == nil {
			item.Input = map[string]any{}
		}
		fragment, _ := item.Input["$arguments_delta"].(string)
		item.Input["$arguments_delta"] = fragment + event.ArgumentsDelta
		if item.ToolUseID == "" {
			item.ToolUseID = event.ToolUseID
		}
		if item.Name == "" {
			item.Name = event.Name
		}
		return nil
	case OutputEventItemCompleted:
		_, err := a.item(event.ItemID, event.ItemKind)
		return err
	case OutputEventCompleted:
		a.finishReason = event.FinishReason
		a.usage = event.Usage
		return nil
	default:
		return fmt.Errorf("output event kind %q is unsupported", event.Kind)
	}
}

func (a *OutputAssembler) Output() CanonicalOutputValue {
	items := cloneCanonicalItems(a.items)
	for i := range items {
		if items[i].Kind != ItemKindToolUse || items[i].Input == nil {
			continue
		}
		raw, _ := items[i].Input["$arguments_delta"].(string)
		delete(items[i].Input, "$arguments_delta")
		if raw == "" {
			continue
		}
		parsed := map[string]any{}
		if err := json.Unmarshal([]byte(raw), &parsed); err == nil {
			items[i].Input = parsed
		}
	}
	return NewOutputWithUsage(a.semanticKind, a.resultID, a.model, items, a.finishReason, a.usage)
}

func (a *OutputAssembler) item(itemID string, expected ItemKind) (*CanonicalItem, error) {
	idx, ok := a.indexByID[itemID]
	if !ok {
		return nil, fmt.Errorf("output item %q does not exist", itemID)
	}
	if expected != "" && a.items[idx].Kind != expected {
		return nil, fmt.Errorf("output item %q kind mismatch", itemID)
	}
	return &a.items[idx], nil
}
