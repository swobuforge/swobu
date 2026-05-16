package canonical

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
)

// ClosedEnvelope is a fully observed envelope and all descendant events needed
// to project canonical snapshots.
type ClosedEnvelope struct {
	ID     EnvelopeID
	Kind   EnvelopeKind
	Events []Event
}

type envelopeOpenProjection struct {
	kind   EnvelopeKind
	parent EnvelopeID
	evs    []Event
}

// ReadClosedEnvelope consumes events until the requested envelope kind closes.
// It returns io.EOF when no such closed envelope exists in the stream.
func ReadClosedEnvelope(ctx context.Context, r EventReader, kind EnvelopeKind) (*ClosedEnvelope, error) {
	open := map[EnvelopeID]*envelopeOpenProjection{}
	appendToAncestors := func(id EnvelopeID, ev Event) {
		current := id
		for current != "" {
			state, ok := open[current]
			if !ok {
				break
			}
			state.evs = append(state.evs, ev)
			current = state.parent
		}
	}
	for {
		ev, err := r.Next(ctx)
		if err == io.EOF {
			return nil, io.EOF
		}
		if err != nil {
			return nil, err
		}
		switch ev.Kind {
		case EventEnvelopeStart:
			payload, ok := ev.Payload.(EnvelopeStartPayload)
			if !ok {
				return nil, fmt.Errorf("envelope.start payload type %T is unsupported", ev.Payload)
			}
			open[ev.EnvID] = &envelopeOpenProjection{kind: payload.Kind, parent: ev.ParentID, evs: nil}
			appendToAncestors(ev.EnvID, ev)
		case EventEnvelopeEnd:
			payload, ok := ev.Payload.(EnvelopeEndPayload)
			if !ok {
				return nil, fmt.Errorf("envelope.end payload type %T is unsupported", ev.Payload)
			}
			state, ok := open[ev.EnvID]
			if !ok {
				return nil, fmt.Errorf("close for unknown envelope %q", ev.EnvID)
			}
			appendToAncestors(ev.EnvID, ev)
			delete(open, ev.EnvID)
			if payload.Kind == kind {
				return &ClosedEnvelope{ID: ev.EnvID, Kind: kind, Events: state.evs}, nil
			}
		default:
			appendToAncestors(ev.EnvID, ev)
		}
	}
}

// ProjectResponse materializes a closed response envelope into canonical output.
// Events remain source of truth; this is a derived view.
func (e *ClosedEnvelope) ProjectResponse() (*CanonicalOutputValue, error) {
	if e == nil || e.Kind != EnvResponse {
		return nil, fmt.Errorf("closed envelope is not a response")
	}
	itemsByID := map[EnvelopeID]*CanonicalItem{}
	orderedIDs := make([]EnvelopeID, 0)
	usage := NewUnknownTokenUsage()
	finish := ""
	resultID := ""
	model := ""

	for _, ev := range e.Events {
		switch ev.Kind {
		case EventEnvelopeStart:
			payload, _ := ev.Payload.(EnvelopeStartPayload)
			switch payload.Kind {
			case EnvMessage:
				item := NewTextOutputItem(string(ev.EnvID), "")
				item.Author = payload.Role
				itemsByID[ev.EnvID] = &item
				orderedIDs = append(orderedIDs, ev.EnvID)
			case EnvToolCall:
				toolUseID := payload.ToolUseID
				if toolUseID == "" {
					toolUseID = string(ev.EnvID)
				}
				item := NewToolUseOutputItem(string(ev.EnvID), toolUseID, payload.Name, map[string]any{})
				item.Author = ItemAuthorAssistant
				itemsByID[ev.EnvID] = &item
				orderedIDs = append(orderedIDs, ev.EnvID)
			}
		case EventTextDelta:
			payload, _ := ev.Payload.(TextDeltaPayload)
			if item, ok := itemsByID[ev.EnvID]; ok {
				item.Text += payload.Text
			}
		case EventArgsDelta:
			payload, _ := ev.Payload.(ArgsDeltaPayload)
			if item, ok := itemsByID[ev.EnvID]; ok {
				// Tool-call arguments can arrive in several deltas; preserve raw
				// concatenation until final decode at closure.
				raw, _ := item.Input["$arguments_delta"].(string)
				item.Input["$arguments_delta"] = raw + payload.Args
			}
		case EventUsage:
			payload, ok := ev.Payload.(UsagePayload)
			if ok {
				usage = payload.Usage
			}
		case EventFinish:
			payload, _ := ev.Payload.(FinishPayload)
			finish = payload.Reason
		case EventMetadata:
			payload, _ := ev.Payload.(MetadataPayload)
			if payload.Values != nil {
				if payload.Values["result_id"] != "" {
					resultID = payload.Values["result_id"]
				}
				if payload.Values["model"] != "" {
					model = payload.Values["model"]
				}
			}
		}
	}
	items := make([]CanonicalItem, 0, len(orderedIDs))
	for _, id := range orderedIDs {
		item := itemsByID[id]
		if item == nil {
			continue
		}
		if item.Kind == ItemKindToolUse && item.Input != nil {
			raw, _ := item.Input["$arguments_delta"].(string)
			if raw != "" {
				decoded := map[string]any{}
				// Keep raw arguments if decode fails; do not invent semantics.
				if err := json.Unmarshal([]byte(raw), &decoded); err == nil {
					item.Input = decoded
				}
			}
		}
		items = append(items, item.Clone())
	}
	out := NewConversationOutputWithUsage(resultID, model, items, finish, usage)
	return &out, nil
}

// ProjectRequest materializes a closed request envelope into a canonical
// request snapshot while preserving semantic kind hints.
func (e *ClosedEnvelope) ProjectRequest() (CanonicalRequest, error) {
	if e == nil || e.Kind != EnvRequest {
		return nil, fmt.Errorf("closed envelope is not a request")
	}
	var (
		model        string
		semanticKind = "conversation"
		itemsByID    = map[EnvelopeID]*CanonicalItem{}
		orderedIDs   = make([]EnvelopeID, 0)
	)
	for _, ev := range e.Events {
		switch ev.Kind {
		case EventMetadata:
			payload, _ := ev.Payload.(MetadataPayload)
			if payload.Values != nil {
				if payload.Values["model"] != "" {
					model = payload.Values["model"]
				}
				if payload.Values["semantic_kind"] != "" {
					semanticKind = payload.Values["semantic_kind"]
				}
			}
		case EventEnvelopeStart:
			payload, _ := ev.Payload.(EnvelopeStartPayload)
			switch payload.Kind {
			case EnvMessage:
				item := NewTextItem(payload.Role, "")
				itemsByID[ev.EnvID] = &item
				orderedIDs = append(orderedIDs, ev.EnvID)
			case EnvToolResult:
				item := NewToolResultItem(payload.Role, payload.ToolUseID, "")
				item.Name = payload.Name
				itemsByID[ev.EnvID] = &item
				orderedIDs = append(orderedIDs, ev.EnvID)
			}
		case EventTextDelta:
			payload, _ := ev.Payload.(TextDeltaPayload)
			if item, ok := itemsByID[ev.EnvID]; ok {
				item.Text += payload.Text
			}
		case EventArgsDelta:
			payload, _ := ev.Payload.(ArgsDeltaPayload)
			if item, ok := itemsByID[ev.EnvID]; ok {
				if item.Input == nil {
					item.Input = map[string]any{}
				}
				// Arguments are folded at close so this path stays stream-safe.
				raw, _ := item.Input["$arguments_delta"].(string)
				item.Input["$arguments_delta"] = raw + payload.Args
			}
		}
	}
	items := make([]CanonicalItem, 0, len(orderedIDs))
	for _, id := range orderedIDs {
		item := itemsByID[id]
		if item == nil {
			continue
		}
		if item.Input != nil {
			if raw, _ := item.Input["$arguments_delta"].(string); raw != "" {
				decoded := map[string]any{}
				if err := json.Unmarshal([]byte(raw), &decoded); err == nil {
					item.Input = decoded
					item.Kind = ItemKindToolUse
				}
				// Remove folding scratch key from projected snapshot.
				delete(item.Input, "$arguments_delta")
			}
		}
		items = append(items, item.Clone())
	}
	switch semanticKind {
	case "response_generation":
		return NewGenerationRequest(GenerationRequestParams{Model: model, Thread: items, LastTurn: items}), nil
	case "prompt_generation":
		prompt := ""
		for _, item := range items {
			if item.Kind == ItemKindText {
				prompt += item.Text
			}
		}
		return NewPromptRequest(model, prompt), nil
	default:
		return NewDialogRequest(model, items), nil
	}
}
