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
func ReadClosedEnvelope(ctx context.Context, r ResponseStream, kind EnvelopeKind) (*ClosedEnvelope, error) {
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
func (e *ClosedEnvelope) ProjectResponse() (*CanonicalOutputProjection, error) {
	if e == nil || e.Kind != EnvResponse {
		return nil, fmt.Errorf("closed envelope is not a response")
	}
	itemsByID := map[EnvelopeID]*CanonicalItem{}
	toolArgsByID := map[EnvelopeID]string{}
	orderedIDs := make([]EnvelopeID, 0)
	usage := NewUnknownTokenUsage()
	finish := ""
	response := ResponseRef{}
	model := ""

	for _, ev := range e.Events {
		if err := responseProjectionApplyEvent(ev, itemsByID, toolArgsByID, &orderedIDs, &usage, &finish, &response, &model); err != nil {
			return nil, err
		}
	}
	items := make([]CanonicalItem, 0, len(orderedIDs))
	for _, id := range orderedIDs {
		item := itemsByID[id]
		if item == nil {
			continue
		}
		if item.Kind() == ItemKindToolUse {
			if raw := toolArgsByID[id]; raw != "" {
				updated, err := withToolUseInput(*item, NewToolArgumentsObject(raw))
				if err != nil {
					return nil, err
				}
				item = &updated
			}
		}
		items = append(items, item.Clone())
	}
	out := newConversationOutputWithResponse(response, model, items, finish, usage)
	return &out, nil
}

func responseProjectionApplyEvent(
	ev Event,
	itemsByID map[EnvelopeID]*CanonicalItem,
	toolArgsByID map[EnvelopeID]string,
	orderedIDs *[]EnvelopeID,
	usage *TokenUsage,
	finish *string,
	response *ResponseRef,
	model *string,
) error {
	switch ev.Kind {
	case EventEnvelopeStart:
		return responseProjectionHandleEnvelopeStart(ev, itemsByID, orderedIDs, response)
	case EventTextDelta, EventArgsDelta:
		return projectionApplyItemDelta(ev, itemsByID, toolArgsByID)
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
			if payload.Values["model"] != "" {
				*model = payload.Values["model"]
			}
		}
	default:
		// ignored by response projection
	}
	return nil
}

func responseProjectionHandleEnvelopeStart(ev Event, itemsByID map[EnvelopeID]*CanonicalItem, orderedIDs *[]EnvelopeID, response *ResponseRef) error {
	payload, ok := ev.Payload.(EnvelopeStartPayload)
	if !ok {
		return fmt.Errorf("envelope start payload type %T is unsupported", ev.Payload)
	}
	if payload.Kind == EnvResponse {
		*response = payload.Response.Clone()
		return nil
	}
	if payload.Kind == EnvMessage {
		item := NewTextOutputItem(string(ev.EnvID), "")
		item = itemWithAuthor(item, payload.Role)
		itemsByID[ev.EnvID] = &item
		*orderedIDs = append(*orderedIDs, ev.EnvID)
		return nil
	}
	if payload.Kind == EnvToolCall {
		toolUseID := payload.ToolUseID
		if toolUseID == "" {
			toolUseID = string(ev.EnvID)
		}
		item := NewToolUseOutputItem(string(ev.EnvID), toolUseID, payload.Name, EmptyToolArguments())
		item, err := withToolUseType(item, payload.ToolType)
		if err != nil {
			return err
		}
		item = itemWithAuthor(item, ItemAuthorAssistant)
		itemsByID[ev.EnvID] = &item
		*orderedIDs = append(*orderedIDs, ev.EnvID)
	}
	return nil
}

// ProjectRequest materializes a closed request envelope into a canonical
// request snapshot while preserving semantic kind hints.
func (e *ClosedEnvelope) ProjectRequest() (CanonicalRequest, error) {
	if e == nil || e.Kind != EnvRequest {
		return CanonicalRequest{}, fmt.Errorf("closed envelope is not a request")
	}
	var (
		model            string
		toolsRaw         string
		toolPolicy       ToolPolicy
		toolCallBatchRaw string
		toolCallBatch    ToolCallBatchPolicy
		controlsRaw      string
		outputFormatRaw  string
		itemsByID        = map[EnvelopeID]*CanonicalItem{}
		toolArgsByID     = map[EnvelopeID]string{}
		orderedIDs       = make([]EnvelopeID, 0)
	)
	for _, ev := range e.Events {
		if err := requestProjectionApplyEvent(ev, itemsByID, toolArgsByID, &orderedIDs, &model, &toolsRaw, &toolPolicy, &toolCallBatchRaw, &controlsRaw, &outputFormatRaw); err != nil {
			return CanonicalRequest{}, err
		}
	}
	items := make([]CanonicalItem, 0, len(orderedIDs))
	for _, id := range orderedIDs {
		item := itemsByID[id]
		if item == nil {
			continue
		}
		if raw := toolArgsByID[id]; raw != "" {
			updated, err := withToolUseInput(*item, NewToolArgumentsObject(raw))
			if err != nil {
				return CanonicalRequest{}, err
			}
			item = &updated
		}
		items = append(items, item.Clone())
	}
	tools, err := decodeRequestToolDeclsMetadata(toolsRaw)
	if err != nil {
		return CanonicalRequest{}, err
	}
	toolCallBatch, err = decodeToolCallBatchMetadata(toolCallBatchRaw)
	if err != nil {
		return CanonicalRequest{}, err
	}
	controls, err := decodeGenerationControlsMetadata(controlsRaw)
	if err != nil {
		return CanonicalRequest{}, err
	}
	outputFormat, err := decodeOutputFormatMetadata(outputFormatRaw)
	if err != nil {
		return CanonicalRequest{}, err
	}
	return NewCanonicalRequest(RequestParams{Model: model, Items: items, Tools: tools, ToolPolicy: toolPolicy, ToolCallBatch: toolCallBatch, Controls: controls, OutputFormat: outputFormat}), nil
}

func requestProjectionApplyEvent(
	ev Event,
	itemsByID map[EnvelopeID]*CanonicalItem,
	toolArgsByID map[EnvelopeID]string,
	orderedIDs *[]EnvelopeID,
	model *string,
	toolsRaw *string,
	toolPolicy *ToolPolicy,
	toolCallBatchRaw *string,
	controlsRaw *string,
	outputFormatRaw *string,
) error {
	switch ev.Kind {
	case EventMetadata:
		payload, _ := ev.Payload.(MetadataPayload)
		if payload.Values != nil {
			if payload.Values["model"] != "" {
				*model = payload.Values["model"]
			}
			if payload.Values["tools"] != "" {
				*toolsRaw = payload.Values["tools"]
			}
			if payload.Values["tool_policy"] != "" {
				policy, err := decodeToolPolicyMetadata(payload.Values["tool_policy"])
				if err != nil {
					return err
				}
				*toolPolicy = policy
			}
			if payload.Values["tool_call_batch"] != "" {
				*toolCallBatchRaw = payload.Values["tool_call_batch"]
			}
			if payload.Values["generation_controls"] != "" {
				*controlsRaw = payload.Values["generation_controls"]
			}
			if payload.Values["output_format"] != "" {
				*outputFormatRaw = payload.Values["output_format"]
			}
		}
	case EventEnvelopeStart:
		return requestProjectionHandleEnvelopeStart(ev, itemsByID, orderedIDs)
	case EventTextDelta, EventArgsDelta:
		return projectionApplyItemDelta(ev, itemsByID, toolArgsByID)
	default:
		// ignored by request projection
	}
	return nil
}

func projectionApplyItemDelta(ev Event, itemsByID map[EnvelopeID]*CanonicalItem, toolArgsByID map[EnvelopeID]string) error {
	item, ok := itemsByID[ev.EnvID]
	if !ok {
		return fmt.Errorf("%s targets unknown canonical item %q", ev.Kind, ev.EnvID)
	}
	switch ev.Kind {
	case EventTextDelta:
		payload, ok := ev.Payload.(TextDeltaPayload)
		if !ok {
			return fmt.Errorf("text delta payload type %T is unsupported", ev.Payload)
		}
		var updated CanonicalItem
		var err error
		switch item.Kind() {
		case ItemKindText:
			updated, err = appendTextItemDelta(*item, payload.Text)
		case ItemKindToolResult:
			updated, err = appendToolResultTextDelta(*item, payload.Text)
		default:
			err = fmt.Errorf("text delta cannot target %q item", item.Kind())
		}
		if err != nil {
			return err
		}
		itemsByID[ev.EnvID] = &updated
	case EventArgsDelta:
		payload, ok := ev.Payload.(ArgsDeltaPayload)
		if !ok {
			return fmt.Errorf("args delta payload type %T is unsupported", ev.Payload)
		}
		if item.Kind() != ItemKindToolUse {
			return fmt.Errorf("args delta cannot target %q item", item.Kind())
		}
		toolArgsByID[ev.EnvID] += payload.Args
	}
	return nil
}

func requestProjectionHandleEnvelopeStart(ev Event, itemsByID map[EnvelopeID]*CanonicalItem, orderedIDs *[]EnvelopeID) error {
	payload, ok := ev.Payload.(EnvelopeStartPayload)
	if !ok {
		return fmt.Errorf("envelope start payload type %T is unsupported", ev.Payload)
	}
	if payload.Kind == EnvMessage {
		item := NewTextItem(payload.Role, "")
		itemsByID[ev.EnvID] = &item
		*orderedIDs = append(*orderedIDs, ev.EnvID)
		return nil
	}
	if payload.Kind == EnvToolCall {
		item := NewToolUseItem(payload.Role, string(ev.EnvID), payload.ToolUseID, payload.Name, EmptyToolArguments())
		var err error
		item, err = withToolUseType(item, payload.ToolType)
		if err != nil {
			return err
		}
		itemsByID[ev.EnvID] = &item
		*orderedIDs = append(*orderedIDs, ev.EnvID)
		return nil
	}
	if payload.Kind == EnvToolResult {
		item := NewToolResultItem(payload.Role, payload.ToolUseID, "")
		itemsByID[ev.EnvID] = &item
		*orderedIDs = append(*orderedIDs, ev.EnvID)
	}
	return nil
}
