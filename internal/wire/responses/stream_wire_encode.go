package responses

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	sse "github.com/swobuforge/swobu/internal/wire/framing/sse"
)

type ResponseStreamWireEncoder struct {
	nextOutputIndex int
	responseID      string
	model           string
	textItem        *responsesTextItemState
	toolItems       map[string]*responsesToolItemState
	outputItems     []any
	request         canonical.CanonicalRequest
}

const (
	responsesStreamCompletedWithoutOutputItemsCode    = "stream_completed_without_output_items"
	responsesStreamCompletedWithoutOutputItemsMessage = "provider stream completed without output items"
)

type responsesTextItemState struct {
	itemID      string
	outputIndex int
	parts       map[uint32]*strings.Builder
	annotations map[uint32][]responsesAnnotationDTO
}

type responsesToolItemState struct {
	itemID      string
	outputIndex int
	callID      string
	name        string
	toolType    string
	arguments   strings.Builder
	webAction   json.RawMessage
}

func NewResponseStreamWireEncoder(request ...canonical.CanonicalRequest) ResponseStreamWireEncoder {
	var effective canonical.CanonicalRequest
	if len(request) > 0 {
		effective = request[0].Clone()
	}
	return ResponseStreamWireEncoder{
		toolItems: map[string]*responsesToolItemState{},
		request:   effective,
	}
}

func (e *ResponseStreamWireEncoder) Encode(event sse.StreamEvent) ([][]byte, error) {
	switch event.Kind {
	case sse.StreamEventStarted:
		e.responseID = sse.FallbackID(event.ResultID, "resp_swobu")
		e.model = event.Model
		raw, err := json.Marshal(responsesCreatedEventDTO{
			Type: "response.created",
			Response: responsesStreamingResponseDTO{
				ID:     e.responseID,
				Object: "response",
				Model:  e.model,
				Status: "in_progress",
				Output: []any{},
			},
		})
		if err != nil {
			return nil, canonical.InternalError("responses event encoding failed")
		}
		return [][]byte{raw}, nil
	case sse.StreamEventItemStarted:
		return e.encodeItemStarted(event)
	case sse.StreamEventContentStarted:
		return e.encodeContentStarted(event)
	case sse.StreamEventTextDelta:
		return e.encodeTextDelta(event)
	case sse.StreamEventToolUseArgumentsDelta:
		return e.encodeToolArgumentsDelta(event)
	case sse.StreamEventItemCompleted:
		return e.encodeItemCompleted(event)
	case sse.StreamEventCompleted:
		frames, err := e.flushOpenItems()
		if err != nil {
			return nil, err
		}
		status, incompleteReason := responsesWireStatusForCompletion(event.Completion)
		if len(e.outputItems) == 0 && trimmedResponseString(event.Completion.Reason()) == "" {
			failed, err := e.encodeFailed(event, responsesStreamCompletedWithoutOutputItemsCode, responsesStreamCompletedWithoutOutputItemsMessage)
			if err != nil {
				return nil, err
			}
			return append(frames, failed...), nil
		}
		eventType := "response.completed"
		responseStatus := status
		if status == "incomplete" {
			eventType = "response.incomplete"
		}
		done, err := json.Marshal(responsesCompletedEventDTO{
			Type: eventType,
			Response: responsesStreamingResponseDTO{
				ID:                sse.FallbackID(e.responseID, "resp_swobu"),
				Object:            "response",
				Model:             e.model,
				Status:            responseStatus,
				IncompleteDetails: responsesIncompleteDetailsForStatus(responseStatus, incompleteReason),
				Output:            e.output(),
				Usage:             responsesUsageFromCanonical(event.Usage),
			},
		})
		if err != nil {
			return nil, canonical.InternalError("responses event encoding failed")
		}
		return append(frames, done), nil
	case sse.StreamEventFailed:
		failed, err := e.encodeFailed(event, "stream_error", "output stream failed")
		if err != nil {
			return nil, err
		}
		return failed, nil
	default:
		return nil, nil
	}
}

func (e *ResponseStreamWireEncoder) Finish() ([][]byte, error) {
	if e == nil {
		return nil, nil
	}
	if e.textItem == nil && len(e.toolItems) == 0 {
		return nil, nil
	}
	return e.flushOpenItems()
}

func (e *ResponseStreamWireEncoder) output() []any {
	if len(e.outputItems) == 0 {
		return []any{}
	}
	return e.outputItems
}

func (e *ResponseStreamWireEncoder) encodeFailed(event sse.StreamEvent, defaultCode string, defaultMessage string) ([][]byte, error) {
	failed, err := json.Marshal(responsesCompletedEventDTO{
		Type: "response.failed",
		Response: responsesStreamingResponseDTO{
			ID:     sse.FallbackID(e.responseID, "resp_swobu"),
			Object: "response",
			Model:  e.model,
			Status: "failed",
			Output: e.output(),
			Error: &responsesErrorDTO{
				Code:    sse.FallbackID(event.ErrorCode, defaultCode),
				Message: sse.FallbackID(event.ErrorMessage, defaultMessage),
			},
		},
	})
	if err != nil {
		return nil, canonical.InternalError("responses event encoding failed")
	}
	return [][]byte{failed}, nil
}

func (e *ResponseStreamWireEncoder) encodeItemStarted(event sse.StreamEvent) ([][]byte, error) {
	switch event.ItemKind {
	case canonical.ItemKindMessage:
		itemID := strings.TrimSpace(event.ItemID) // swobu:io-string source=boundary
		if itemID == "" {
			itemID = "msg_swobu_" + strconv.Itoa(e.nextOutputIndex)
		}
		if e.textItem != nil && e.textItem.itemID == itemID {
			return nil, nil
		}
		frames, err := e.flushOpenTextItem()
		if err != nil {
			return nil, err
		}
		opened, err := e.openTextItem(itemID)
		if err != nil {
			return nil, err
		}
		return append(frames, opened...), nil
	case canonical.ItemKindToolCall:
		return e.openToolItem(event.ItemID, event.ToolUseID, event.Name, event.ToolType)
	case canonical.ItemKindReasoning:
		return nil, nil
	default:
		return nil, nil
	}
}

func (e *ResponseStreamWireEncoder) encodeTextDelta(event sse.StreamEvent) ([][]byte, error) {
	itemID := strings.TrimSpace(event.ItemID) // swobu:io-string source=boundary
	if itemID == "" && e.textItem != nil {
		itemID = e.textItem.itemID
	}
	if itemID == "" {
		itemID = "msg_swobu_" + strconv.Itoa(e.nextOutputIndex)
	}
	frames, err := e.ensureTextItem(itemID)
	if err != nil {
		return nil, err
	}
	if e.textItem == nil {
		return frames, nil
	}
	partFrames, err := e.openTextPart(e.textItem, event.PartOrdinal)
	if err != nil {
		return nil, err
	}
	frames = append(frames, partFrames...)
	e.textItem.parts[event.PartOrdinal].WriteString(event.TextDelta)
	delta, err := json.Marshal(responsesTextDeltaEventDTO{
		Type:         "response.output_text.delta",
		ItemID:       e.textItem.itemID,
		OutputIndex:  e.textItem.outputIndex,
		ContentIndex: int(event.PartOrdinal),
		Delta:        event.TextDelta,
	})
	if err != nil {
		return nil, canonical.InternalError("responses event encoding failed")
	}
	return append(frames, delta), nil
}

func (e *ResponseStreamWireEncoder) encodeContentStarted(event sse.StreamEvent) ([][]byte, error) {
	if event.PartKind != canonical.PartKindText {
		return nil, canonical.NewBackendError(
			"responses",
			0,
			"Responses client stream cannot represent the backend image response",
			"",
		)
	}
	frames, err := e.ensureTextItem(event.ItemID)
	if err != nil || e.textItem == nil {
		return frames, err
	}
	partFrames, err := e.openTextPart(e.textItem, event.PartOrdinal)
	return append(frames, partFrames...), err
}

func (e *ResponseStreamWireEncoder) encodeToolArgumentsDelta(event sse.StreamEvent) ([][]byte, error) {
	itemID := strings.TrimSpace(event.ItemID) // swobu:io-string source=boundary
	if itemID == "" {
		itemID = "fc_swobu_" + strconv.Itoa(e.nextOutputIndex)
	}
	frames, err := e.ensureToolItem(itemID, event.ToolUseID, event.Name, event.ToolType)
	if err != nil {
		return nil, err
	}
	state := e.toolItems[itemID]
	if state == nil {
		return frames, nil
	}
	if state.toolType == canonical.ToolTypeWebSearch {
		return nil, canonical.InternalError("canonical Responses web-search stream contains function arguments")
	}
	if state.callID == "" {
		state.callID = event.ToolUseID
	}
	if state.name == "" {
		state.name = event.Name
	}
	state.arguments.WriteString(event.ArgumentsDelta)
	deltaType := "response.function_call_arguments.delta"
	if strings.ToLower(strings.TrimSpace(state.toolType)) == canonical.ToolTypeCustom { // swobu:io-string source=domain
		deltaType = "response.custom_tool_call_input.delta"
	}
	delta, err := json.Marshal(responsesToolArgumentsDeltaEventDTO{
		Type:        deltaType,
		ItemID:      state.itemID,
		OutputIndex: state.outputIndex,
		CallID:      state.callID,
		Name:        state.name,
		Delta:       event.ArgumentsDelta,
	})
	if err != nil {
		return nil, canonical.InternalError("responses event encoding failed")
	}
	return append(frames, delta), nil
}

func (e *ResponseStreamWireEncoder) encodeItemCompleted(event sse.StreamEvent) ([][]byte, error) {
	switch event.ItemKind {
	case canonical.ItemKindMessage:
		itemID := strings.TrimSpace(event.ItemID) // swobu:io-string source=boundary
		if e.textItem != nil && (itemID == "" || itemID == e.textItem.itemID) {
			return e.completeTextItem(event.CompletedItem)
		}
		return nil, nil
	case canonical.ItemKindToolCall:
		itemID := strings.TrimSpace(event.ItemID) // swobu:io-string source=boundary
		if itemID == "" {
			return nil, nil
		}
		if state := e.toolItems[itemID]; state != nil && state.toolType == canonical.ToolTypeWebSearch {
			return e.completeWebSearchCall(itemID, event.CompletedItem)
		}
		return e.closeToolItem(itemID)
	case canonical.ItemKindToolResult:
		return e.completeWebSearchResult(event.CompletedItem)
	case canonical.ItemKindReasoning:
		return e.closeReasoningItem(event)
	default:
		return nil, nil
	}
}

func (e *ResponseStreamWireEncoder) closeReasoningItem(event sse.StreamEvent) ([][]byte, error) {
	if event.CompletedItem == nil {
		return nil, canonical.InternalError("responses reasoning completion is missing canonical item")
	}
	outputIndex := e.nextOutputIndex
	e.nextOutputIndex++
	projection := responsesResponseHistoryState{}
	if err := projection.appendItem(e.request, int(event.ItemOrdinal), *event.CompletedItem); err != nil {
		return nil, err
	}
	item := projection.items[0]
	frames := make([][]byte, 0, len(item.Summary)+2)
	added := item
	added.Status = "in_progress"
	added.Summary = nil
	added.Content = nil
	raw, err := json.Marshal(responsesOutputItemEventDTO{Type: "response.output_item.added", OutputIndex: outputIndex, Item: added})
	if err != nil {
		return nil, canonical.InternalError("responses reasoning event encoding failed")
	}
	frames = append(frames, raw)
	for index, part := range item.Summary {
		raw, err = json.Marshal(map[string]any{
			"type": "response.reasoning_summary_part.added", "item_id": item.ID,
			"output_index": outputIndex, "summary_index": index, "part": part,
		})
		if err != nil {
			return nil, canonical.InternalError("responses reasoning event encoding failed")
		}
		frames = append(frames, raw)
	}
	raw, err = json.Marshal(responsesOutputItemEventDTO{Type: "response.output_item.done", OutputIndex: outputIndex, Item: item})
	if err != nil {
		return nil, canonical.InternalError("responses reasoning event encoding failed")
	}
	frames = append(frames, raw)
	e.outputItems = append(e.outputItems, item)
	return frames, nil
}

func (e *ResponseStreamWireEncoder) ensureTextItem(itemID string) ([][]byte, error) {
	if e.textItem != nil && e.textItem.itemID == itemID {
		return nil, nil
	}
	frames, err := e.flushOpenTextItem()
	if err != nil {
		return nil, err
	}
	opened, err := e.openTextItem(itemID)
	if err != nil {
		return nil, err
	}
	return append(frames, opened...), nil
}

func (e *ResponseStreamWireEncoder) openTextItem(itemID string) ([][]byte, error) {
	state := &responsesTextItemState{
		itemID:      itemID,
		outputIndex: e.nextOutputIndex,
		parts:       map[uint32]*strings.Builder{},
		annotations: map[uint32][]responsesAnnotationDTO{},
	}
	e.nextOutputIndex++
	e.textItem = state
	added, err := json.Marshal(responsesOutputItemEventDTO{
		Type:        "response.output_item.added",
		OutputIndex: state.outputIndex,
		Item: responsesOutputItemMessageDTO{
			ID:      state.itemID,
			Type:    "message",
			Status:  "in_progress",
			Role:    "assistant",
			Content: []responsesOutputTextStreamDTO{},
		},
	})
	if err != nil {
		return nil, canonical.InternalError("responses event encoding failed")
	}
	return [][]byte{added}, nil
}

func (e *ResponseStreamWireEncoder) openTextPart(state *responsesTextItemState, ordinal uint32) ([][]byte, error) {
	if state.parts[ordinal] != nil {
		return nil, nil
	}
	state.parts[ordinal] = &strings.Builder{}
	part, err := json.Marshal(responsesContentPartEventDTO{
		Type:         "response.content_part.added",
		ItemID:       state.itemID,
		OutputIndex:  state.outputIndex,
		ContentIndex: int(ordinal),
		Part: responsesOutputTextStreamDTO{
			Type:        "output_text",
			Text:        "",
			Annotations: []responsesAnnotationDTO{},
		},
	})
	if err != nil {
		return nil, canonical.InternalError("responses event encoding failed")
	}
	return [][]byte{part}, nil
}

func (e *ResponseStreamWireEncoder) flushOpenTextItem() ([][]byte, error) {
	if e.textItem == nil {
		return nil, nil
	}
	state := e.textItem
	ordinals := make([]int, 0, len(state.parts))
	for ordinal := range state.parts {
		ordinals = append(ordinals, int(ordinal))
	}
	sort.Ints(ordinals)
	frames := make([][]byte, 0, len(ordinals)*2+1)
	content := make([]responsesOutputTextStreamDTO, 0, len(ordinals))
	for _, rawOrdinal := range ordinals {
		text := state.parts[uint32(rawOrdinal)].String()
		textDone, err := json.Marshal(responsesTextDoneEventDTO{Type: "response.output_text.done", ItemID: state.itemID, OutputIndex: state.outputIndex, ContentIndex: rawOrdinal, Text: text})
		if err != nil {
			return nil, canonical.InternalError("responses event encoding failed")
		}
		annotations := state.annotations[uint32(rawOrdinal)]
		if annotations == nil {
			annotations = []responsesAnnotationDTO{}
		}
		partValue := responsesOutputTextStreamDTO{Type: "output_text", Text: text, Annotations: annotations}
		partDone, err := json.Marshal(responsesContentPartEventDTO{Type: "response.content_part.done", ItemID: state.itemID, OutputIndex: state.outputIndex, ContentIndex: rawOrdinal, Part: partValue})
		if err != nil {
			return nil, canonical.InternalError("responses event encoding failed")
		}
		frames = append(frames, textDone, partDone)
		content = append(content, partValue)
	}
	itemDone, err := json.Marshal(responsesOutputItemEventDTO{
		Type:        "response.output_item.done",
		OutputIndex: state.outputIndex,
		Item: responsesOutputItemMessageDTO{
			ID:      state.itemID,
			Type:    "message",
			Status:  "completed",
			Role:    "assistant",
			Content: content,
		},
	})
	if err != nil {
		return nil, canonical.InternalError("responses event encoding failed")
	}
	e.outputItems = append(e.outputItems, responsesOutputItemMessageDTO{
		ID:      state.itemID,
		Type:    "message",
		Status:  "completed",
		Role:    "assistant",
		Content: content,
	})
	e.textItem = nil
	return append(frames, itemDone), nil
}

func (e *ResponseStreamWireEncoder) completeTextItem(completed *canonical.CanonicalItem) ([][]byte, error) {
	if e.textItem == nil {
		return nil, nil
	}
	if completed == nil {
		return nil, canonical.InternalError("responses message completion is missing canonical item")
	}
	message, ok := completed.Message()
	if !ok {
		return nil, canonical.InternalError("responses message completion has invalid canonical item")
	}
	for ordinal, part := range message.Content() {
		text, ok := part.Text()
		if !ok {
			return nil, canonical.NewBackendError(
				"responses",
				0,
				"Responses client stream cannot represent the backend image response",
				"",
			)
		}
		annotations, err := encodeResponsesAnnotations(text.Text(), part.Citations())
		if err != nil {
			return nil, err
		}
		e.textItem.annotations[uint32(ordinal)] = annotations
	}
	return e.flushOpenTextItem()
}
