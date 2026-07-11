package canonical

import (
	"context"
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
func (e *ClosedEnvelope) ProjectResponse() (*CanonicalOutputData, error) {
	if e == nil || e.Kind != EnvResponse {
		return nil, fmt.Errorf("closed envelope is not a response")
	}
	itemsByID := map[EnvelopeID]*CanonicalItem{}
	toolArgsByID := map[EnvelopeID]string{}
	orderedIDs := make([]EnvelopeID, 0)
	usage := NewUnknownTokenUsage()
	finish := ""
	resultID := ""
	model := ""

	for _, ev := range e.Events {
		responseProjectionApplyEvent(ev, itemsByID, toolArgsByID, &orderedIDs, &usage, &finish, &resultID, &model)
	}
	items := make([]CanonicalItem, 0, len(orderedIDs))
	for _, id := range orderedIDs {
		item := itemsByID[id]
		if item == nil {
			continue
		}
		if item.Kind == ItemKindToolUse {
			if raw := toolArgsByID[id]; raw != "" {
				item.Input = NewToolArgumentsObject(raw)
			}
		}
		items = append(items, item.Clone())
	}
	out := NewConversationOutputWithUsage(resultID, model, items, finish, usage)
	return &out, nil
}

func responseProjectionApplyEvent(
	ev Event,
	itemsByID map[EnvelopeID]*CanonicalItem,
	toolArgsByID map[EnvelopeID]string,
	orderedIDs *[]EnvelopeID,
	usage *TokenUsage,
	finish *string,
	resultID *string,
	model *string,
) {
	switch ev.Kind {
	case EventEnvelopeStart:
		responseProjectionHandleEnvelopeStart(ev, itemsByID, orderedIDs)
	case EventTextDelta:
		payload, _ := ev.Payload.(TextDeltaPayload)
		if item, ok := itemsByID[ev.EnvID]; ok {
			item.Text += payload.Text
		}
	case EventArgsDelta:
		payload, _ := ev.Payload.(ArgsDeltaPayload)
		if _, ok := itemsByID[ev.EnvID]; ok {
			toolArgsByID[ev.EnvID] = toolArgsByID[ev.EnvID] + payload.Args
		}
	case EventUsage:
		payload, ok := ev.Payload.(UsagePayload)
		if ok {
			*usage = payload.Usage
		}
	case EventFinish:
		payload, _ := ev.Payload.(FinishPayload)
		*finish = payload.Reason
	case EventMetadata:
		payload, _ := ev.Payload.(MetadataPayload)
		if payload.Values != nil {
			if payload.Values["result_id"] != "" {
				*resultID = payload.Values["result_id"]
			}
			if payload.Values["model"] != "" {
				*model = payload.Values["model"]
			}
		}
	default:
		// ignored by response projection
	}
}

func responseProjectionHandleEnvelopeStart(ev Event, itemsByID map[EnvelopeID]*CanonicalItem, orderedIDs *[]EnvelopeID) {
	payload, _ := ev.Payload.(EnvelopeStartPayload)
	if payload.Kind == EnvMessage {
		item := NewTextOutputItem(string(ev.EnvID), "")
		item.Author = payload.Role
		itemsByID[ev.EnvID] = &item
		*orderedIDs = append(*orderedIDs, ev.EnvID)
		return
	}
	if payload.Kind == EnvToolCall {
		toolUseID := payload.ToolUseID
		if toolUseID == "" {
			toolUseID = string(ev.EnvID)
		}
		item := NewToolUseOutputItem(string(ev.EnvID), toolUseID, payload.Name, EmptyToolArguments())
		item.Author = ItemAuthorAssistant
		itemsByID[ev.EnvID] = &item
		*orderedIDs = append(*orderedIDs, ev.EnvID)
	}
}

// ProjectRequest materializes a closed request envelope into a canonical
// request snapshot while preserving semantic kind hints.
func (e *ClosedEnvelope) ProjectRequest() (CanonicalRequest, error) {
	if e == nil || e.Kind != EnvRequest {
		return CanonicalRequest{}, fmt.Errorf("closed envelope is not a request")
	}
	var (
		model        string
		itemsByID    = map[EnvelopeID]*CanonicalItem{}
		toolArgsByID = map[EnvelopeID]string{}
		orderedIDs   = make([]EnvelopeID, 0)
	)
	for _, ev := range e.Events {
		requestProjectionApplyEvent(ev, itemsByID, toolArgsByID, &orderedIDs, &model)
	}
	items := make([]CanonicalItem, 0, len(orderedIDs))
	for _, id := range orderedIDs {
		item := itemsByID[id]
		if item == nil {
			continue
		}
		if raw := toolArgsByID[id]; raw != "" {
			item.Input = NewToolArgumentsObject(raw)
			item.Kind = ItemKindToolUse
		}
		items = append(items, item.Clone())
	}
	return NewCanonicalRequest(RequestParams{Model: model, Items: items}), nil
}

func requestProjectionApplyEvent(
	ev Event,
	itemsByID map[EnvelopeID]*CanonicalItem,
	toolArgsByID map[EnvelopeID]string,
	orderedIDs *[]EnvelopeID,
	model *string,
) {
	switch ev.Kind {
	case EventMetadata:
		payload, _ := ev.Payload.(MetadataPayload)
		if payload.Values != nil {
			if payload.Values["model"] != "" {
				*model = payload.Values["model"]
			}
		}
	case EventEnvelopeStart:
		requestProjectionHandleEnvelopeStart(ev, itemsByID, orderedIDs)
	case EventTextDelta:
		payload, _ := ev.Payload.(TextDeltaPayload)
		if item, ok := itemsByID[ev.EnvID]; ok {
			item.Text += payload.Text
		}
	case EventArgsDelta:
		payload, _ := ev.Payload.(ArgsDeltaPayload)
		if _, ok := itemsByID[ev.EnvID]; ok {
			toolArgsByID[ev.EnvID] = toolArgsByID[ev.EnvID] + payload.Args
		}
	default:
		// ignored by request projection
	}
}

func requestProjectionHandleEnvelopeStart(ev Event, itemsByID map[EnvelopeID]*CanonicalItem, orderedIDs *[]EnvelopeID) {
	payload, _ := ev.Payload.(EnvelopeStartPayload)
	if payload.Kind == EnvMessage {
		item := NewTextItem(payload.Role, "")
		itemsByID[ev.EnvID] = &item
		*orderedIDs = append(*orderedIDs, ev.EnvID)
		return
	}
	if payload.Kind == EnvToolResult {
		item := NewToolResultItem(payload.Role, payload.ToolUseID, "")
		item.Name = payload.Name
		itemsByID[ev.EnvID] = &item
		*orderedIDs = append(*orderedIDs, ev.EnvID)
	}
}
