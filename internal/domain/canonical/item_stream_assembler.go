package canonical

import (
	"fmt"
	"sort"
	"strings"
)

type itemStreamState struct {
	start     ItemStartPayload
	args      string
	parts     map[uint32]*itemStreamPartState
	completed *CanonicalItem
}

type itemStreamPartState struct {
	kind PartKind
	// text accumulates streamed text deltas for one part. A strings.Builder
	// keeps accumulation O(n) in total delta bytes rather than O(n^2): Go's
	// `+=` on a string reallocates and copies the whole growing prefix on every
	// delta, which dominates memory for long streamed responses (epic-50). The
	// accumulated value is only read once, at EventItemCompleted validation.
	text strings.Builder
}

// itemStreamAssembler validates progressive start/delta facts against the
// completed canonical checkpoints without using wire aliases or CallID as the
// routing key.
type itemStreamAssembler struct {
	items map[uint32]*itemStreamState
}

func newItemStreamAssembler() *itemStreamAssembler {
	return &itemStreamAssembler{items: make(map[uint32]*itemStreamState)}
}

// swobu:lint ignore function-complexity because=item lifecycle validation stays centralized across the five typed event transitions.
func (a *itemStreamAssembler) apply(kind EventKind, event ItemEvent) error {
	if event.Payload == nil {
		return fmt.Errorf("%s item ordinal %d has no payload", kind, event.Position.Item)
	}
	state := a.items[event.Position.Item]
	switch kind {
	case EventItemStart:
		start, ok := event.Payload.(ItemStartPayload)
		if !ok || start.Kind() == "" {
			return fmt.Errorf("item.start ordinal %d has invalid payload %T", event.Position.Item, event.Payload)
		}
		if state != nil {
			return fmt.Errorf("item.start ordinal %d is duplicated", event.Position.Item)
		}
		a.items[event.Position.Item] = &itemStreamState{start: start, parts: make(map[uint32]*itemStreamPartState)}
	case EventArgsDelta:
		delta, ok := event.Payload.(ArgsDeltaPayload)
		if !ok {
			return fmt.Errorf("args.delta ordinal %d has payload %T", event.Position.Item, event.Payload)
		}
		if state == nil || state.start.Kind() != ItemKindToolCall || state.completed != nil {
			return fmt.Errorf("args.delta ordinal %d has no open tool-call start", event.Position.Item)
		}
		state.args += delta.Args
	case EventContentStart, EventTextDelta:
		if state == nil || state.completed != nil || state.start.Kind() != ItemKindMessage {
			return fmt.Errorf("%s ordinal %d has no open content-bearing item start", kind, event.Position.Item)
		}
		if kind == EventContentStart {
			content, ok := event.Payload.(ContentStartPayload)
			if !ok {
				return fmt.Errorf("content.start ordinal %d has payload %T", event.Position.Item, event.Payload)
			}
			if _, exists := state.parts[event.Position.Part]; exists {
				return fmt.Errorf("content.start item ordinal %d part ordinal %d is duplicated", event.Position.Item, event.Position.Part)
			}
			if event.Position.Part != uint32(len(state.parts)) {
				return fmt.Errorf("content.start item ordinal %d part ordinal %d is non-contiguous", event.Position.Item, event.Position.Part)
			}
			part, err := newItemStreamPartState(state.start.Kind(), content)
			if err != nil {
				return fmt.Errorf("content.start item ordinal %d part ordinal %d: %w", event.Position.Item, event.Position.Part, err)
			}
			state.parts[event.Position.Part] = part
		} else {
			delta, ok := event.Payload.(TextDeltaPayload)
			if !ok {
				return fmt.Errorf("text.delta ordinal %d has payload %T", event.Position.Item, event.Payload)
			}
			part := state.parts[event.Position.Part]
			if part == nil || !part.acceptsText() {
				return fmt.Errorf("text.delta item ordinal %d part ordinal %d has no open text content", event.Position.Item, event.Position.Part)
			}
			part.text.WriteString(delta.Text)
		}
	case EventItemCompleted:
		completed, ok := event.Payload.(ItemCompletedPayload)
		if !ok || completed.Item.Kind() == "" {
			return fmt.Errorf("item.completed ordinal %d has invalid payload %T", event.Position.Item, event.Payload)
		}
		if state == nil {
			if completed.Item.Kind() != ItemKindToolResult &&
				completed.Item.Kind() != ItemKindToolDiscoveryResult &&
				completed.Item.Kind() != ItemKindReasoning {
				return fmt.Errorf("item.completed ordinal %d has no start", event.Position.Item)
			}
			state = &itemStreamState{}
			a.items[event.Position.Item] = state
		}
		if state.completed != nil {
			return fmt.Errorf("item.completed ordinal %d is duplicated", event.Position.Item)
		}
		if err := validateCompletedItem(state, completed.Item); err != nil {
			return fmt.Errorf("item.completed ordinal %d: %w", event.Position.Item, err)
		}
		item := completed.Item.Clone()
		state.completed = &item
	default:
		return fmt.Errorf("item event kind %q is unsupported", kind)
	}
	return nil
}

func newItemStreamPartState(parent ItemKind, content ContentStartPayload) (*itemStreamPartState, error) {
	switch parent {
	case ItemKindMessage:
		if content.Kind == "" {
			return nil, fmt.Errorf("message content kind is empty")
		}
		return &itemStreamPartState{kind: content.Kind}, nil
	default:
		return nil, fmt.Errorf("item kind %q cannot contain content", parent)
	}
}

func (p *itemStreamPartState) acceptsText() bool {
	return p != nil && p.kind == PartKindText
}

func validateCompletedItem(state *itemStreamState, item CanonicalItem) error {
	if (item.Kind() == ItemKindToolResult ||
		item.Kind() == ItemKindToolDiscoveryResult ||
		item.Kind() == ItemKindReasoning) &&
		state.start.Kind() == "" {
		return nil
	}
	if item.Kind() != state.start.Kind() {
		return fmt.Errorf("completed kind %q does not match started kind %q", item.Kind(), state.start.Kind())
	}
	// swobu:lint ignore switch-exhaustive because=tool results return above and aliases duplicate the message/tool-call enum values.
	switch item.Kind() {
	case ItemKindMessage:
		messageStart, _ := state.start.Message()
		message, _ := item.Message()
		if message.Role() != messageStart.Author {
			return fmt.Errorf("completed author %q does not match started author %q", message.Role(), messageStart.Author)
		}
		content := message.Content()
		if len(content) != len(state.parts) {
			return fmt.Errorf("completed content part count %d does not match streamed part count %d", len(content), len(state.parts))
		}
		for ordinal, completedPart := range content {
			streamed := state.parts[uint32(ordinal)]
			if streamed == nil || completedPart.Kind() != streamed.kind {
				return fmt.Errorf("completed content part %d kind does not match streamed content", ordinal)
			}
			if text, ok := completedPart.Text(); ok && text.Text() != streamed.text.String() {
				return fmt.Errorf("completed content part %d text does not match streamed text", ordinal)
			}
		}
	case ItemKindToolCall:
		toolStart, _ := state.start.ToolCall()
		call, _ := item.ToolCall()
		if call.CallID() != toolStart.CallID {
			return fmt.Errorf("completed CallID %q does not match started CallID %q", call.CallID().String(), toolStart.CallID.String())
		}
		if call.Tool() != toolStart.Tool {
			return fmt.Errorf("completed tool reference does not match started tool reference")
		}
		if object, ok := call.Input().Object(); ok {
			projected, err := ParseJSONObject([]byte(state.args))
			if err != nil || projected.String() != object.String() {
				return fmt.Errorf("completed object arguments do not match streamed deltas")
			}
		} else if text, ok := call.Input().Text(); ok {
			if text != state.args {
				return fmt.Errorf("completed text arguments do not match streamed deltas")
			}
		} else if _, ok := call.Input().WebSearch(); ok {
			if state.args != "" {
				return fmt.Errorf("completed web-search input conflicts with untyped argument deltas")
			}
		} else {
			return fmt.Errorf("completed tool input has no supported branch")
		}
	default:
		return fmt.Errorf("completed item kind %q is unsupported", item.Kind())
	}
	return nil
}

func (a *itemStreamAssembler) completedItems() ([]CanonicalItem, error) {
	ordinals := make([]int, 0, len(a.items))
	for ordinal, state := range a.items {
		if state.completed == nil {
			return nil, fmt.Errorf("item ordinal %d has no ItemCompleted checkpoint", ordinal)
		}
		ordinals = append(ordinals, int(ordinal))
	}
	sort.Ints(ordinals)
	for index, ordinal := range ordinals {
		if ordinal != index {
			return nil, fmt.Errorf("item ordinal %d is non-contiguous; expected %d", ordinal, index)
		}
	}
	items := make([]CanonicalItem, 0, len(ordinals))
	for _, ordinal := range ordinals {
		items = append(items, a.items[uint32(ordinal)].completed.Clone())
	}
	return items, nil
}
