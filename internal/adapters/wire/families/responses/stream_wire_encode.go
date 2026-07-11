package responses

import (
	"encoding/json"
	"strconv"
	"strings"

	sse "github.com/swobuforge/swobu/internal/adapters/wire/framing/sse"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

type ResponseStreamWireEncoder struct {
	nextOutputIndex int
	responseID      string
	model           string
	textItem        *responsesTextItemState
	toolItems       map[string]*responsesToolItemState
	outputItems     []any
}

type responsesTextItemState struct {
	itemID      string
	outputIndex int
	content     strings.Builder
}

type responsesToolItemState struct {
	itemID      string
	outputIndex int
	callID      string
	name        string
	arguments   strings.Builder
}

func NewResponseStreamWireEncoder() ResponseStreamWireEncoder {
	return ResponseStreamWireEncoder{
		toolItems: map[string]*responsesToolItemState{},
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
		done, err := json.Marshal(responsesCompletedEventDTO{
			Type: "response.completed",
			Response: responsesStreamingResponseDTO{
				ID:     sse.FallbackID(e.responseID, "resp_swobu"),
				Object: "response",
				Model:  e.model,
				Status: "completed",
				Output: e.outputItems,
				Usage:  responsesUsageFromCanonical(event.Usage),
			},
		})
		if err != nil {
			return nil, canonical.InternalError("responses event encoding failed")
		}
		return append(frames, done), nil
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

func (e *ResponseStreamWireEncoder) encodeItemStarted(event sse.StreamEvent) ([][]byte, error) {
	switch event.ItemKind {
	case canonical.ItemKindText:
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
	case canonical.ItemKindToolUse:
		return e.openToolItem(event.ItemID, event.ToolUseID, event.Name)
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
	e.textItem.content.WriteString(event.TextDelta)
	delta, err := json.Marshal(responsesTextDeltaEventDTO{
		Type:         "response.output_text.delta",
		ItemID:       e.textItem.itemID,
		OutputIndex:  e.textItem.outputIndex,
		ContentIndex: 0,
		Delta:        event.TextDelta,
	})
	if err != nil {
		return nil, canonical.InternalError("responses event encoding failed")
	}
	return append(frames, delta), nil
}

func (e *ResponseStreamWireEncoder) encodeToolArgumentsDelta(event sse.StreamEvent) ([][]byte, error) {
	itemID := strings.TrimSpace(event.ItemID) // swobu:io-string source=boundary
	if itemID == "" {
		itemID = "fc_swobu_" + strconv.Itoa(e.nextOutputIndex)
	}
	frames, err := e.ensureToolItem(itemID, event.ToolUseID, event.Name)
	if err != nil {
		return nil, err
	}
	state := e.toolItems[itemID]
	if state == nil {
		return frames, nil
	}
	if state.callID == "" {
		state.callID = event.ToolUseID
	}
	if state.name == "" {
		state.name = event.Name
	}
	state.arguments.WriteString(event.ArgumentsDelta)
	delta, err := json.Marshal(responsesToolArgumentsDeltaEventDTO{
		Type:        "response.function_call_arguments.delta",
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
	case canonical.ItemKindText:
		itemID := strings.TrimSpace(event.ItemID) // swobu:io-string source=boundary
		if e.textItem != nil && (itemID == "" || itemID == e.textItem.itemID) {
			return e.flushOpenTextItem()
		}
		return nil, nil
	case canonical.ItemKindToolUse:
		itemID := strings.TrimSpace(event.ItemID) // swobu:io-string source=boundary
		if itemID == "" {
			return nil, nil
		}
		return e.closeToolItem(itemID)
	default:
		return nil, nil
	}
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
	part, err := json.Marshal(responsesContentPartEventDTO{
		Type:         "response.content_part.added",
		ItemID:       state.itemID,
		OutputIndex:  state.outputIndex,
		ContentIndex: 0,
		Part: responsesOutputTextStreamDTO{
			Type:        "output_text",
			Text:        "",
			Annotations: []any{},
		},
	})
	if err != nil {
		return nil, canonical.InternalError("responses event encoding failed")
	}
	return [][]byte{added, part}, nil
}

func (e *ResponseStreamWireEncoder) flushOpenTextItem() ([][]byte, error) {
	if e.textItem == nil {
		return nil, nil
	}
	state := e.textItem
	text := state.content.String()
	textDone, err := json.Marshal(responsesTextDoneEventDTO{
		Type:         "response.output_text.done",
		ItemID:       state.itemID,
		OutputIndex:  state.outputIndex,
		ContentIndex: 0,
		Text:         text,
	})
	if err != nil {
		return nil, canonical.InternalError("responses event encoding failed")
	}
	partDone, err := json.Marshal(responsesContentPartEventDTO{
		Type:         "response.content_part.done",
		ItemID:       state.itemID,
		OutputIndex:  state.outputIndex,
		ContentIndex: 0,
		Part: responsesOutputTextStreamDTO{
			Type:        "output_text",
			Text:        text,
			Annotations: []any{},
		},
	})
	if err != nil {
		return nil, canonical.InternalError("responses event encoding failed")
	}
	itemDone, err := json.Marshal(responsesOutputItemEventDTO{
		Type:        "response.output_item.done",
		OutputIndex: state.outputIndex,
		Item: responsesOutputItemMessageDTO{
			ID:     state.itemID,
			Type:   "message",
			Status: "completed",
			Role:   "assistant",
			Content: []responsesOutputTextStreamDTO{{
				Type:        "output_text",
				Text:        text,
				Annotations: []any{},
			}},
		},
	})
	if err != nil {
		return nil, canonical.InternalError("responses event encoding failed")
	}
	e.outputItems = append(e.outputItems, responsesOutputItemMessageDTO{
		ID:     state.itemID,
		Type:   "message",
		Status: "completed",
		Role:   "assistant",
		Content: []responsesOutputTextStreamDTO{{
			Type:        "output_text",
			Text:        text,
			Annotations: []any{},
		}},
	})
	e.textItem = nil
	return [][]byte{textDone, partDone, itemDone}, nil
}
