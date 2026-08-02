package responses

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func (e *ResponseStreamWireEncoder) ensureToolItem(itemID string, callID string, name string, toolType string) ([][]byte, error) {
	if state := e.toolItems[itemID]; state != nil {
		return nil, nil
	}
	return e.openToolItem(itemID, callID, name, "", toolType)
}

func (e *ResponseStreamWireEncoder) openToolItem(itemID string, callID string, name string, namespace string, toolType string) ([][]byte, error) {
	if strings.TrimSpace(itemID) == "" { // swobu:io-string source=boundary
		itemID = "fc_swobu_" + strconv.Itoa(e.nextOutputIndex)
	}
	if state := e.toolItems[itemID]; state != nil {
		if state.callID == "" {
			state.callID = callID
		}
		if state.name == "" {
			state.name = name
		}
		if state.namespace == "" {
			state.namespace = namespace
		}
		return nil, nil
	}
	normalizedType := strings.ToLower(strings.TrimSpace(toolType)) // swobu:io-string source=boundary
	if normalizedType == "" {
		normalizedType = canonical.ToolTypeFunction
	}
	state := &responsesToolItemState{
		itemID:      itemID,
		outputIndex: e.nextOutputIndex,
		callID:      callID,
		name:        name,
		namespace:   namespace,
		toolType:    normalizedType,
	}
	e.nextOutputIndex++
	e.toolItems[itemID] = state
	added, err := json.Marshal(responsesOutputItemEventDTO{
		Type:        "response.output_item.added",
		OutputIndex: state.outputIndex,
		Item:        responsesWireToolItem(state.itemID, state.callID, state.name, state.namespace, state.toolType, "in_progress", ""),
	})
	if err != nil {
		return nil, canonical.InternalError("responses event encoding failed")
	}
	return [][]byte{added}, nil
}

func (e *ResponseStreamWireEncoder) closeToolItem(itemID string) ([][]byte, error) {
	state := e.toolItems[itemID]
	if state == nil {
		return nil, nil
	}
	if state.toolType == canonical.ToolTypeWebSearch {
		return e.closeWebSearchItem(itemID, state.webAction)
	}
	args := state.arguments.String()
	argsDoneType := "response.function_call_arguments.done"
	argsDone := responsesToolArgumentsDoneEventDTO{
		Type:        argsDoneType,
		ItemID:      state.itemID,
		OutputIndex: state.outputIndex,
		CallID:      state.callID,
		Name:        state.name,
		Namespace:   responsesClientNamespaceValue(state.namespace),
	}
	if state.toolType == canonical.ToolTypeCustom {
		argsDoneType = "response.custom_tool_call_input.done"
		argsDone.Input = args
	} else {
		argsDone.Arguments = args
	}
	argsDone.Type = argsDoneType
	// Keep function-call arguments OpenAI-shaped so OpenAI-family clients can
	// consume the stream without a field-name shim.
	argsDoneBytes, err := json.Marshal(argsDone)
	if err != nil {
		return nil, canonical.InternalError("responses event encoding failed")
	}
	itemDone, err := json.Marshal(responsesOutputItemEventDTO{
		Type:        "response.output_item.done",
		OutputIndex: state.outputIndex,
		Item:        responsesWireToolItem(state.itemID, state.callID, state.name, state.namespace, state.toolType, "completed", args),
	})
	if err != nil {
		return nil, canonical.InternalError("responses event encoding failed")
	}
	e.outputItems = append(e.outputItems, responsesWireToolItem(state.itemID, state.callID, state.name, state.namespace, state.toolType, "completed", args))
	delete(e.toolItems, itemID)
	return [][]byte{argsDoneBytes, itemDone}, nil
}

func (e *ResponseStreamWireEncoder) flushOpenItems() ([][]byte, error) {
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

func responsesWireToolItem(itemID string, callID string, name string, namespace string, toolType string, status string, payload string) any {
	normalizedType := strings.ToLower(strings.TrimSpace(toolType)) // swobu:io-string source=boundary
	if normalizedType == canonical.ToolTypeWebSearch {
		if strings.TrimSpace(callID) != "" { // swobu:io-string source=domain
			itemID = callID
		}
		return responsesWireOutputItemDTO{ID: itemID, Type: "web_search_call", Status: status}
	}
	if normalizedType == canonical.ToolTypeCustom {
		return responsesOutputItemCustomToolCallDTO{
			ID:     itemID,
			Type:   "custom_tool_call",
			Status: status,
			CallID: callID,
			Name:   name,
			Input:  payload,
		}
	}
	return responsesOutputItemFunctionCallDTO{
		ID:        itemID,
		Type:      "function_call",
		Status:    status,
		CallID:    callID,
		Name:      name,
		Namespace: responsesClientNamespaceValue(namespace),
		Arguments: payload,
	}
}

func (e *ResponseStreamWireEncoder) completeWebSearchCall(itemID string, completed *canonical.CanonicalItem) ([][]byte, error) {
	state := e.toolItems[itemID]
	if state == nil || completed == nil {
		return nil, canonical.InternalError("responses web-search stream call is incomplete")
	}
	call, ok := completed.ToolCall()
	if !ok {
		return nil, canonical.InternalError("responses web-search stream call checkpoint is invalid")
	}
	search, ok := call.Input().WebSearch()
	if !ok {
		return nil, canonical.InternalError("responses web-search stream call lacks typed input")
	}
	action, err := encodeResponsesWebSearchAction(search)
	if err != nil {
		return nil, err
	}
	state.webAction = action
	return nil, nil
}

func (e *ResponseStreamWireEncoder) completeWebSearchResult(completed *canonical.CanonicalItem) ([][]byte, error) {
	if completed == nil {
		return nil, nil
	}
	result, ok := completed.ToolResult()
	if !ok {
		return nil, nil
	}
	search, ok := result.WebSearch()
	if !ok {
		return nil, nil
	}
	for itemID, state := range e.toolItems {
		if state.toolType != canonical.ToolTypeWebSearch || state.callID != result.CallID().String() {
			continue
		}
		if len(state.webAction) == 0 {
			return nil, canonical.InternalError("responses web-search stream result precedes its call checkpoint")
		}
		action, err := encodeResponsesWebSearchSources(search, state.webAction)
		if err != nil {
			return nil, err
		}
		return e.closeWebSearchItem(itemID, action)
	}
	return nil, canonical.InternalError("responses web-search stream result has no prior call")
}

func (e *ResponseStreamWireEncoder) closeWebSearchItem(itemID string, action json.RawMessage) ([][]byte, error) {
	state := e.toolItems[itemID]
	if state == nil {
		return nil, nil
	}
	if len(action) == 0 {
		return nil, canonical.InternalError("responses web-search stream call has no completed action")
	}
	wireID := state.callID
	if strings.TrimSpace(wireID) == "" { // swobu:io-string source=domain
		wireID = state.itemID
	}
	item := responsesWireOutputItemDTO{ID: wireID, Type: "web_search_call", Status: "completed", Action: action}
	done, err := json.Marshal(responsesOutputItemEventDTO{Type: "response.output_item.done", OutputIndex: state.outputIndex, Item: item})
	if err != nil {
		return nil, canonical.InternalError("responses web-search event encoding failed")
	}
	e.outputItems = append(e.outputItems, item)
	delete(e.toolItems, itemID)
	return [][]byte{done}, nil
}
