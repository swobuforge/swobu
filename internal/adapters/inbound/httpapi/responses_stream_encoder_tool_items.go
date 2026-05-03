package httpapi

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/compatibility"
)

func (e *responsesClientStreamEncoderWire) ensureToolItem(itemID string, callID string, name string) ([][]byte, error) {
	if state := e.toolItems[itemID]; state != nil {
		return nil, nil
	}
	return e.openToolItem(itemID, callID, name)
}

func (e *responsesClientStreamEncoderWire) openToolItem(itemID string, callID string, name string) ([][]byte, error) {
	if strings.TrimSpace(itemID) == "" {
		itemID = "fc_swobu_" + strconv.Itoa(e.nextOutputIndex)
	}
	if state := e.toolItems[itemID]; state != nil {
		if state.callID == "" {
			state.callID = callID
		}
		if state.name == "" {
			state.name = name
		}
		return nil, nil
	}
	state := &responsesToolItemState{
		itemID:      itemID,
		outputIndex: e.nextOutputIndex,
		callID:      callID,
		name:        name,
	}
	e.nextOutputIndex++
	e.toolItems[itemID] = state
	added, err := json.Marshal(responsesOutputItemEventDTO{
		Type:        "response.output_item.added",
		OutputIndex: state.outputIndex,
		Item: responsesOutputItemFunctionCallDTO{
			ID:        state.itemID,
			Type:      "function_call",
			Status:    "in_progress",
			CallID:    state.callID,
			Name:      state.name,
			Arguments: "",
		},
	})
	if err != nil {
		return nil, compatibility.InternalError("responses event encoding failed")
	}
	return [][]byte{added}, nil
}

func (e *responsesClientStreamEncoderWire) closeToolItem(itemID string) ([][]byte, error) {
	state := e.toolItems[itemID]
	if state == nil {
		return nil, nil
	}
	args := state.arguments.String()
	argsDone, err := json.Marshal(responsesToolArgumentsDoneEventDTO{
		Type:        "response.function_call_arguments.done",
		ItemID:      state.itemID,
		OutputIndex: state.outputIndex,
		CallID:      state.callID,
		Name:        state.name,
	})
	if err != nil {
		return nil, compatibility.InternalError("responses event encoding failed")
	}
	itemDone, err := json.Marshal(responsesOutputItemEventDTO{
		Type:        "response.output_item.done",
		OutputIndex: state.outputIndex,
		Item: responsesOutputItemFunctionCallDTO{
			ID:        state.itemID,
			Type:      "function_call",
			Status:    "completed",
			CallID:    state.callID,
			Name:      state.name,
			Arguments: args,
		},
	})
	if err != nil {
		return nil, compatibility.InternalError("responses event encoding failed")
	}
	e.outputItems = append(e.outputItems, responsesOutputItemFunctionCallDTO{
		ID:        state.itemID,
		Type:      "function_call",
		Status:    "completed",
		CallID:    state.callID,
		Name:      state.name,
		Arguments: args,
	})
	delete(e.toolItems, itemID)
	return [][]byte{argsDone, itemDone}, nil
}

func (e *responsesClientStreamEncoderWire) flushOpenItems() ([][]byte, error) {
	var frames [][]byte
	textFrames, err := e.flushOpenTextItem()
	if err != nil {
		return nil, err
	}
	frames = append(frames, textFrames...)
	itemIDs := make([]string, 0, len(e.toolItems))
	for itemID := range e.toolItems {
		itemIDs = append(itemIDs, itemID)
	}
	sort.Strings(itemIDs)
	for _, itemID := range itemIDs {
		toolFrames, err := e.closeToolItem(itemID)
		if err != nil {
			return nil, err
		}
		frames = append(frames, toolFrames...)
	}
	return frames, nil
}
