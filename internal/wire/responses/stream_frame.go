package responses

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

type streamFrame struct {
	RawItem      json.RawMessage   `json:"-"`
	RawOutput    []json.RawMessage `json:"-"`
	EventIndex   int               `json:"-"`
	Type         string            `json:"type"`
	ID           string            `json:"id"`
	Model        string            `json:"model"`
	Delta        string            `json:"delta"`
	Input        string            `json:"input"`
	Status       string            `json:"status"`
	Code         string            `json:"code"`
	Message      string            `json:"message"`
	CallID       string            `json:"call_id"`
	Name         string            `json:"name"`
	ItemID       string            `json:"item_id"`
	OutputIndex  *int              `json:"output_index"`
	SummaryIndex *int              `json:"summary_index"`
	ContentIndex *int              `json:"content_index"`
	Arguments    string            `json:"arguments"`
	Response     struct {
		ID                string                         `json:"id"`
		Model             string                         `json:"model"`
		Status            string                         `json:"status"`
		Store             *bool                          `json:"store"`
		IncompleteDetails *responsesIncompleteDetailsDTO `json:"incomplete_details,omitempty"`
		ContentFilters    []responsesContentFilterDTO    `json:"content_filters,omitempty"`
		Output            []json.RawMessage              `json:"output,omitempty"`
		OutputText        string                         `json:"output_text,omitempty"`
		Error             struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	} `json:"response"`
	Item struct {
		ID               string                         `json:"id"`
		Type             string                         `json:"type"`
		CallID           string                         `json:"call_id"`
		Name             string                         `json:"name"`
		Arguments        string                         `json:"arguments"`
		Input            string                         `json:"input"`
		ServerLabel      string                         `json:"server_label"`
		Status           string                         `json:"status"`
		Summary          []responsesReasoningSummaryDTO `json:"summary"`
		Content          json.RawMessage                `json:"content"`
		EncryptedContent string                         `json:"encrypted_content"`
		Action           json.RawMessage                `json:"action"`
	} `json:"item"`
}

// swobu:lint ignore function-complexity because=Responses streaming dispatch keeps frame ordering and lifecycle ownership in one boundary function.
func (s *responsesResponseStream) handleFrame(ctx context.Context, frame streamFrame) (bool, canonical.Event, error) {
	frameType := strings.TrimSpace(frame.Type) // swobu:io-string source=provider-wire
	var completedAdmission responsesOutputAdmission
	switch frameType {
	case "response.output_item.added":
		if strings.TrimSpace(frame.Item.Type) == "" {
			return false, canonical.Event{}, canonical.NewBackendError("responses", 0, "responses output item is missing type", "")
		}
	case "response.output_item.done":
		admission, err := admitCompletedResponsesOutputItem(responsesWireOutputItemDTO{
			Type:   frame.Item.Type,
			Status: frame.Item.Status,
		}, "")
		if err != nil {
			return false, canonical.Event{}, err
		}
		completedAdmission = admission
	}
	if responsesCarriesOutputLifecycle(frameType) {
		frozenDuplicate, err := s.acceptOutputFrame(frameType, frame)
		if err != nil {
			return false, canonical.Event{}, err
		}
		if frozenDuplicate {
			if frameType == "response.output_item.done" && len(frame.RawItem) > 0 {
				var item responsesWireOutputItemDTO
				if err := json.Unmarshal(frame.RawItem, &item); err != nil {
					return false, canonical.Event{}, canonical.InternalError("responses duplicate completed item is invalid JSON")
				}
				if err := s.validateCheckpoint(ctx, *frame.OutputIndex, frame.RawItem, item, "completed", false); err != nil {
					return false, canonical.Event{}, err
				}
			}
			return true, canonical.Event{}, nil
		}
	}
	if frameType == "response.output_item.done" && completedAdmission.disposition == responsesOutputDeferPartial {
		if len(frame.RawItem) == 0 {
			return false, canonical.Event{}, canonical.InternalError("responses deferred completed item is missing")
		}
		state := s.outputAt(*frame.OutputIndex)
		if len(state.terminalPendingItem) > 0 {
			return false, canonical.Event{}, canonical.NewBackendError("responses", 0, "responses output item repeated before terminal response", "")
		}
		state.terminalPendingItem = append(json.RawMessage(nil), frame.RawItem...)
		state.phase = responsesOutputAwaitingTerminal
		return true, canonical.Event{}, nil
	}
	if frameType == "response.output_item.done" && completedAdmission.disposition == responsesOutputErase {
		s.outputAt(*frame.OutputIndex).checkpointStatus = responsesCompletedItemStatus(frame.Item.Status)
		if err := s.eraseProviderOutput(frame, completedAdmission.eraseField); err != nil {
			return false, canonical.Event{}, err
		}
		return true, canonical.Event{}, nil
	}
	switch frameType {
	case "response.created":
		if err := s.handleResponseCreated(ctx, frame); err != nil {
			return false, canonical.Event{}, err
		}
		return true, canonical.Event{}, nil
	case "response.output_text.delta":
		if err := s.handleOutputTextDelta(frame); err != nil {
			return false, canonical.Event{}, err
		}
		return true, canonical.Event{}, nil
	case "response.reasoning_summary_text.delta":
		if err := s.handleReasoningDelta(frame, canonical.ReasoningPartSummary); err != nil {
			return false, canonical.Event{}, err
		}
		return true, canonical.Event{}, nil
	case "response.reasoning_text.delta":
		if err := s.handleReasoningDelta(frame, canonical.ReasoningPartTrace); err != nil {
			return false, canonical.Event{}, err
		}
		return true, canonical.Event{}, nil
	case "response.function_call_arguments.delta":
		if err := s.handleToolArgumentsDelta(frame, canonical.ToolTypeFunction); err != nil {
			return false, canonical.Event{}, err
		}
		return true, canonical.Event{}, nil
	case "response.mcp_call_arguments.delta":
		return false, canonical.Event{}, responsesProviderMCPContradiction()
	case "response.custom_tool_call_input.delta":
		if err := s.handleToolArgumentsDelta(frame, canonical.ToolTypeCustom); err != nil {
			return false, canonical.Event{}, err
		}
		return true, canonical.Event{}, nil
	case "response.output_item.added":
		if strings.TrimSpace(frame.Item.Type) == "mcp_call" {
			return false, canonical.Event{}, responsesProviderMCPContradiction()
		}
		if strings.TrimSpace(frame.Item.Type) == "web_search_call" { // swobu:io-string source=provider-wire
			return true, canonical.Event{}, nil
		}
		handled, err := s.handleOutputItemAdded(frame)
		if err != nil {
			return false, canonical.Event{}, err
		}
		if !handled {
			return false, canonical.Event{}, nil
		}
		return true, canonical.Event{}, nil
	case "response.function_call_arguments.done":
		if _, err := s.handleToolArgumentsDone(frame, canonical.ToolTypeFunction); err != nil {
			return false, canonical.Event{}, err
		}
		return true, canonical.Event{}, nil
	case "response.mcp_call_arguments.done":
		return false, canonical.Event{}, responsesProviderMCPContradiction()
	case "response.custom_tool_call_input.done":
		if _, err := s.handleToolArgumentsDone(frame, canonical.ToolTypeCustom); err != nil {
			return false, canonical.Event{}, err
		}
		return true, canonical.Event{}, nil
	case "response.output_item.done":
		s.outputAt(*frame.OutputIndex).checkpointStatus = responsesCompletedItemStatus(frame.Item.Status)
		handled, err := s.handleOutputItemDone(ctx, frame)
		if err != nil {
			return false, canonical.Event{}, err
		}
		s.checkpointOutput(frame.OutputIndex)
		if !handled {
			return false, canonical.Event{}, nil
		}
		return true, canonical.Event{}, nil
	case "response.completed":
		if err := s.handleResponseTerminal(ctx, frame); err != nil {
			return false, canonical.Event{}, err
		}
		return true, canonical.Event{}, nil
	case "response.incomplete":
		if err := s.handleResponseTerminal(ctx, frame); err != nil {
			return false, canonical.Event{}, err
		}
		return true, canonical.Event{}, nil
	case "response.failed":
		message := strings.TrimSpace(frame.Response.Error.Message) // swobu:io-string source=provider-wire
		if message == "" {
			message = "responses stream returned response.failed"
		}
		if code := strings.TrimSpace(frame.Response.Error.Code); code != "" { // swobu:io-string source=provider-wire
			message = code + ": " + message
		}
		return false, canonical.Event{}, canonical.NewBackendError("responses", 0, message, "")
	case "error":
		message := strings.TrimSpace(frame.Message) // swobu:io-string source=provider-wire
		if message == "" {
			message = "responses stream returned an error event"
		}
		if code := strings.TrimSpace(frame.Code); code != "" { // swobu:io-string source=provider-wire
			message = code + ": " + message
		}
		return false, canonical.Event{}, canonical.NewBackendError("responses", 0, message, "")
	default:
		key := responsesUnknownEventDecisionKey(frameType, frame.OutputIndex)
		if _, recorded := s.unknownEventDecisions[key]; recorded {
			return true, canonical.Event{}, nil
		}
		if s.unknownEventDecisions == nil {
			s.unknownEventDecisions = make(map[string]struct{})
		}
		s.unknownEventDecisions[key] = struct{}{}
		return true, canonical.Event{}, nil
	}
}

func responsesCarriesOutputLifecycle(frameType string) bool {
	switch frameType {
	case "response.output_text.delta",
		"response.reasoning_summary_text.delta",
		"response.reasoning_text.delta",
		"response.function_call_arguments.delta",
		"response.function_call_arguments.done",
		"response.custom_tool_call_input.delta",
		"response.custom_tool_call_input.done",
		"response.mcp_call_arguments.delta",
		"response.mcp_call_arguments.done",
		"response.output_item.added",
		"response.output_item.done":
		return true
	default:
		return false
	}
}

// responsesRecognizesWireOutputKind classifies known provider syntax,
// independently of whether that kind currently survives canonical projection.
func responsesRecognizesWireOutputKind(itemType string) bool {
	switch strings.TrimSpace(itemType) {
	case "reasoning", "function_call", "custom_tool_call", "message", "web_search_call", "tool_search_call", "tool_search_output", "mcp_call":
		return true
	default:
		return false
	}
}

func responsesProviderMCPContradiction() error {
	return canonical.NewBackendError(
		"responses",
		0,
		"responses provider emitted an MCP effect that the attempted request did not authorize",
		"",
	)
}

func responsesUnknownEventDecisionKey(frameType string, outputIndex *int) string {
	if outputIndex != nil {
		return frameType + "\x00" + fmt.Sprintf("output:%d", *outputIndex)
	}
	return frameType
}

func responsesCompletedItemStatus(status string) string {
	status = strings.TrimSpace(status)
	if status == "" {
		return "completed"
	}
	return status
}

func (s *responsesResponseStream) handleResponseCreated(ctx context.Context, frame streamFrame) error {
	if s.started {
		return nil
	}
	s.started = true
	providerResponseID := frame.ID
	if strings.TrimSpace(providerResponseID) == "" { // swobu:io-string source=provider-wire
		providerResponseID = frame.Response.ID
	}
	if strings.TrimSpace(providerResponseID) == "" { // swobu:io-string source=provider-wire
		return canonical.InternalError("responses stream is missing response id")
	}
	model := strings.TrimSpace(frame.Model) // swobu:io-string source=boundary
	if model == "" {
		model = strings.TrimSpace(frame.Response.Model) // swobu:io-string source=boundary
	}
	s.enqueueEnvelopeStart(s.responseEnvID, "", canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse, Model: model}, canonical.EventMetadataFields{})
	ref := canonical.ResponseRef{}
	if s.continuationEligible && frame.Response.Store != nil && *frame.Response.Store {
		ref.Responses = &canonical.ResponsesContinuation{ProviderResponseID: canonical.NewResponsesResponseID(providerResponseID)}
	}
	s.enqueue(canonical.Event{Kind: canonical.EventResponseIdentity, EnvID: s.responseEnvID, Payload: canonical.ResponseIdentityPayload{Response: ref}})
	return nil
}

func (s *responsesResponseStream) handleOutputTextDelta(frame streamFrame) error {
	outputIndex := *frame.OutputIndex
	output := s.outputAt(outputIndex)
	state := output.text
	if state == nil {
		start, err := canonical.NewMessageStart(canonical.MessageRoleAssistant)
		if err != nil {
			return err
		}
		ordinal := uint32(0)
		state = newResponsesTextState(ordinal)
		output.text = state
		s.enqueueItemStart(frame.OutputIndex, ordinal, start)
	}
	contentIndex := 0
	if frame.ContentIndex != nil {
		contentIndex = *frame.ContentIndex
	}
	if contentIndex < 0 {
		return canonical.NewBackendError("responses", 0, "responses message content index is negative", "")
	}
	part := state.parts[contentIndex]
	if part == nil {
		part = &responsesTextPartState{}
		state.parts[contentIndex] = part
	}
	if part.classified && part.erased {
		return canonical.NewBackendError("responses", 0, "responses message content changed from erased to text", "")
	}
	part.classified = true
	part.text.WriteString(frame.Delta)
	if part.emitted {
		s.enqueueTextDelta(frame.OutputIndex, state.ordinal, part.ordinal, frame.Delta)
		return nil
	}
	part.deltas = append(part.deltas, frame.Delta)
	s.flushMessagePartFrontier(outputIndex, state)
	return nil
}

func (s *responsesResponseStream) handleToolArgumentsDelta(frame streamFrame, toolType string) error {
	outputIndex := *frame.OutputIndex
	ordinal := uint32(0)
	if _, err := s.ensureToolState(outputIndex, ordinal, toolType, frame.CallID, frame.Name); err != nil {
		return err
	}
	s.enqueueToolArgs(outputIndex, frame.Delta)
	return nil
}

func (s *responsesResponseStream) handleOutputItemAdded(frame streamFrame) (bool, error) {
	itemType := strings.TrimSpace(frame.Item.Type) // swobu:io-string source=boundary
	output := s.outputAt(*frame.OutputIndex)
	if itemType == "reasoning" {
		if output.reasoning != nil {
			return false, nil
		}
		output.reasoning = newResponsesReasoningState()
		return true, nil
	}
	var toolType string
	switch itemType {
	case "function_call":
		toolType = canonical.ToolTypeFunction
	case "custom_tool_call":
		toolType = canonical.ToolTypeCustom
	case "message", "web_search_call", "tool_search_call", "tool_search_output":
		return false, nil
	default:
		// Unknown announcements stay open so their done or terminal evidence
		// classifies one erasure at the output frontier.
		output.ignoredAtAdded = true
		return true, nil
	}
	if output.tool != nil {
		return false, nil
	}
	ordinal := uint32(0)
	state, err := s.ensureToolState(*frame.OutputIndex, ordinal, toolType, frame.Item.CallID, frame.Item.Name)
	if err != nil {
		return false, err
	}
	initial := ""
	if toolType == canonical.ToolTypeCustom {
		initial = frame.Item.Input
	} else {
		initial = frame.Item.Arguments
	}
	if initial != "" {
		s.enqueueArgsDelta(frame.OutputIndex, state.ordinal, initial)
		state.input += initial
	}
	return true, nil
}

func (s *responsesResponseStream) handleToolArgumentsDone(frame streamFrame, toolType string) (bool, error) {
	return s.completeToolState(frame, toolType, true, false)
}

func (s *responsesResponseStream) handleOutputItemDone(ctx context.Context, frame streamFrame) (bool, error) {
	itemType := strings.TrimSpace(frame.Item.Type) // swobu:io-string source=boundary
	switch itemType {
	case "reasoning":
		return s.completeReasoningState(frame)
	case "function_call":
		return s.completeToolState(frame, canonical.ToolTypeFunction, false, true)
	case "custom_tool_call":
		return s.completeToolState(frame, canonical.ToolTypeCustom, false, true)
	case "web_search_call":
		rawAction := bytes.TrimSpace(frame.Item.Action)
		if len(rawAction) == 0 || bytes.Equal(rawAction, []byte("null")) {
			if strings.TrimSpace(frame.Item.Status) != "completed" { // swobu:io-string source=provider-wire
				return false, canonical.NewBackendError("responses", 0, "responses streamed actionless web-search marker is not completed", "")
			}
			s.omitProviderOutput(frame.OutputIndex)
			return true, nil
		}
		state, err := decodeResponsesWebSearchLifecycleState(frame.Item.Status)
		if err != nil {
			if err := s.recordUnknownWebSearchStatus(frame); err != nil {
				return false, err
			}
			state = responsesWebSearchUnknown
		}
		return s.completeWebSearchItem(frame, state)
	case "message":
		return s.completeMessageItem(frame)
	default:
		if itemType != "tool_search_call" && itemType != "tool_search_output" {
			return false, canonical.InternalError("responses admitted streamed output disposition has no projector")
		}
		if len(frame.RawItem) == 0 {
			return false, canonical.InternalError("responses completed opaque item is missing")
		}
		index := *frame.OutputIndex
		items, err := decodeCompletedResponsesItemSetAtIndexes(
			ctx,
			s.request,
			s.toolNames,
			[]json.RawMessage{frame.RawItem},
			"",
			[]int{index},
			true,
			"completed",
			s.exchangeID,
			s.changeLog,
		)
		if err != nil {
			return false, err
		}
		if err := s.enqueueCompletedOutputItems(frame.OutputIndex, items); err != nil {
			return false, err
		}
		if len(items) == 0 {
			s.omitProviderOutput(frame.OutputIndex)
		}
		return true, nil
	}
}

func (s *responsesResponseStream) completeToolState(frame streamFrame, toolType string, argumentsDone bool, outputDone bool) (bool, error) {
	outputIndex := *frame.OutputIndex
	output := s.outputAt(outputIndex)
	callID := frame.CallID
	name := frame.Name
	if callID == "" {
		callID = frame.Item.CallID
	}
	if name == "" {
		name = frame.Item.Name
	}
	ordinal := uint32(0)
	state, err := s.ensureToolState(outputIndex, ordinal, toolType, callID, name)
	if err != nil {
		return false, err
	}
	emitted := false
	finalInput := frame.Input
	if finalInput == "" {
		if toolType == canonical.ToolTypeCustom {
			finalInput = frame.Item.Input
		} else {
			finalInput = frame.Arguments
			if finalInput == "" {
				finalInput = frame.Item.Arguments
			}
		}
	}
	if argumentsDone || outputDone {
		suffix, reconcileErr := responsesTerminalSuffix(state.input, finalInput)
		if reconcileErr != nil {
			return false, reconcileErr
		}
		if suffix != "" {
			s.enqueueArgsDelta(frame.OutputIndex, state.ordinal, suffix)
			emitted = true
		}
		state.input = finalInput
	}
	if argumentsDone {
		s.markToolStateArgumentsDone(state)
	}
	if outputDone {
		s.markToolStateOutputDone(state)
	}
	if state.closed {
		if state.argumentsDone && state.outputDone {
			output.tool = nil
		}
		return emitted, nil
	}
	if outputDone {
		var input canonical.ToolInput
		if toolType == canonical.ToolTypeCustom {
			input = canonical.NewTextToolInput(state.input)
		} else {
			object, parseErr := canonical.ParseJSONObject([]byte(state.input))
			if parseErr != nil {
				return false, canonical.InternalError("responses streamed tool arguments are invalid")
			}
			input = canonical.NewJSONObjectToolInput(object)
		}
		item, itemErr := canonical.NewToolCallItem(state.callID, state.tool, input)
		if itemErr != nil {
			return false, canonical.InternalError("responses streamed tool call is invalid")
		}
		s.commitOutputItem(frame.OutputIndex, state.ordinal, item)
		state.closed = true
		emitted = true
		output.tool = nil
		return emitted, nil
	}
	return emitted, nil
}

func (s *responsesResponseStream) handleResponseTerminal(ctx context.Context, frame streamFrame) error {
	if s.completed {
		return nil
	}
	if !s.started {
		if err := s.handleResponseCreated(ctx, frame); err != nil {
			return err
		}
	}
	terminalStatus := responsesTerminalStatus(frame.Type, frame.Status, frame.Response.Status)
	if terminalReason, promptBlocked := responsesTerminalReason(frame.Type, frame.Status, frame.Response.Status, frame.Response.ContentFilters, responseIncompleteReason(frame.Response.IncompleteDetails)); promptBlocked {
		s.completed = true
		s.discardOpenText()
		s.closeOpenTools(canonical.EnvelopeStatusError)
		s.enqueueError("content_filter", responsesContentFilterMessage(responsesBlockedContentFilterSource(frame.Response.ContentFilters)))
		s.enqueueEnvelopeEnd(s.responseEnvID, canonical.EnvResponse, canonical.EnvelopeStatusError)
		return nil
	} else {
		if err := admitResponsesProjectableResponseStatus(terminalStatus); err != nil {
			return err
		}
		usedFallback := false
		fallbackItems := []canonical.CanonicalItem(nil)
		observed := make(map[int]struct{}, len(frame.Response.Output))
		for index, raw := range frame.Response.Output {
			fallback, items, err := s.observeTerminalOutput(ctx, index, raw, terminalStatus)
			if err != nil {
				return err
			}
			observed[index] = struct{}{}
			usedFallback = usedFallback || fallback
			fallbackItems = append(fallbackItems, items...)
		}
		if err := s.settleTerminalOmissions(ctx, observed, terminalStatus); err != nil {
			return err
		}
		if s.hasOpenText() && s.completedItems == 0 && strings.TrimSpace(frame.Response.OutputText) != "" {
			if err := s.completeOpenTextFromTerminal(frame.Response.OutputText); err != nil {
				return err
			}
		}
		if s.completedItems == 0 && !s.hasOpenText() && strings.TrimSpace(frame.Response.OutputText) != "" {
			item, err := canonical.NewMessageItem(canonical.MessageRoleAssistant, []canonical.MessagePart{canonical.NewTextMessagePart(frame.Response.OutputText)})
			if err != nil {
				return canonical.InternalError("responses output text is invalid")
			}
			if err := s.enqueueCompletedOutputItems(nil, []canonical.CanonicalItem{item}); err != nil {
				return err
			}
			usedFallback = true
			fallbackItems = append(fallbackItems, item)
		}
		if s.hasOpenKnownOutput() {
			return canonical.NewBackendError("responses", 0, "responses stream ended with unfinished output", "")
		}
		if s.completedItems == 0 && s.erasedOutput {
			return canonical.NewBackendError("responses", 0, "backend produced no usable canonical output", "")
		}
		s.completed = true
		s.enqueueUsage(s.latestUsage)
		s.enqueueFinish(responsesCompletion(terminalStatus, terminalReason))
		s.enqueueEnvelopeEnd(s.responseEnvID, canonical.EnvResponse, canonical.EnvelopeStatusCompleted)
		logResponsesTerminalProjection(usedFallback, len(frame.Response.Output), strings.TrimSpace(frame.Response.OutputText) != "", fallbackItems) // swobu:io-string source=provider-wire
		return nil
	}
}

func (s *responsesResponseStream) observeTerminalOutput(ctx context.Context, index int, raw json.RawMessage, responseStatus string) (bool, []canonical.CanonicalItem, error) {
	var item responsesWireOutputItemDTO
	if err := json.Unmarshal(raw, &item); err != nil {
		return false, nil, canonical.InternalError("responses terminal output item is invalid JSON")
	}
	if _, err := admitCompletedResponsesOutputItem(item, responseStatus); err != nil {
		return false, nil, err
	}
	if err := s.acceptTerminalOutput(index, item); err != nil {
		return false, nil, err
	}
	slot := s.outputAt(index)
	resolvedRaw, err := restoreResponsesTerminalIdentity(raw, item, slot.identity)
	if err != nil {
		return false, nil, err
	}
	if err := json.Unmarshal(resolvedRaw, &item); err != nil {
		return false, nil, canonical.InternalError("responses terminal output item is invalid JSON")
	}
	switch slot.phase {
	case responsesOutputCheckpointed:
		if err := s.validateCheckpoint(ctx, index, resolvedRaw, item, responseStatus, true); err != nil {
			return false, nil, err
		}
		slot.phase = responsesOutputSettled
		return false, nil, nil
	case responsesOutputAwaitingTerminal:
		if err := s.validatePendingTerminal(ctx, index, resolvedRaw, responseStatus); err != nil {
			return false, nil, err
		}
		if _, _, err := s.completeObservedOutput(ctx, index, resolvedRaw, item, responseStatus); err != nil {
			return false, nil, err
		}
		slot.terminalPendingItem = nil
		slot.phase = responsesOutputSettled
		return false, nil, nil
	case responsesOutputAccumulating:
		fallback, items, err := s.completeObservedOutput(ctx, index, resolvedRaw, item, responseStatus)
		if err != nil {
			return false, nil, err
		}
		slot.phase = responsesOutputSettled
		return fallback, items, nil
	case responsesOutputSettled:
		if err := s.validateCheckpoint(ctx, index, resolvedRaw, item, responseStatus, true); err != nil {
			return false, nil, err
		}
		return false, nil, nil
	default:
		return false, nil, canonical.InternalError("responses output slot has invalid phase")
	}
}

// validateCheckpoint compares one repeated observation with the canonical
// checkpoint already committed by the indexed slot.
func (s *responsesResponseStream) validateCheckpoint(ctx context.Context, index int, raw json.RawMessage, item responsesWireOutputItemDTO, responseStatus string, identityResolved bool) error {
	state := s.outputAt(index)
	resolvedRaw := raw
	var err error
	if !identityResolved {
		resolvedRaw, err = restoreResponsesTerminalIdentity(raw, item, state.identity)
		if err != nil {
			return err
		}
	}
	changeLog := s.changeLog
	if (state.erased && state.erasureRecorded) || state.statusDropRecorded {
		// The resolved top-level erasure already recorded its one occurrence
		// decision, or its status refinement was already omitted. Decoding here
		// is solely semantic validation.
		changeLog = nil
	}
	terminalItems, err := decodeCompletedResponsesItemSetAtIndexes(
		ctx, s.request, s.toolNames, []json.RawMessage{resolvedRaw}, "", []int{index}, true,
		responseStatus, s.exchangeID, changeLog,
	)
	if err != nil {
		return err
	}
	if mismatch := compareResponsesCanonicalItems(state.checkpointItems, terminalItems); mismatch != "" {
		logResponsesTerminalCheckpointMismatch(s.exchangeID, index, state.checkpointItems, terminalItems, mismatch)
		return canonical.NewBackendError("responses", 0, "responses terminal output contradicts completed semantic checkpoint", "")
	}
	if state.checkpointStatus != responsesCompletedItemStatus(item.Status) {
		return canonical.NewBackendError("responses", 0, "responses terminal output status contradicts completed checkpoint", "")
	}
	return nil
}

// restoreResponsesTerminalIdentity makes the indexed output owner authoritative
// when a later terminal snapshot omits identity already validated earlier in
// the lifecycle. It edits only absent fields and retains the original JSON so
// terminal-only additive evidence still reaches semantic projection.
func restoreResponsesTerminalIdentity(raw json.RawMessage, item responsesWireOutputItemDTO, identity committedOutputIdentity) (json.RawMessage, error) {
	fields := map[string]string{
		"id":      identity.itemID,
		"call_id": identity.callID,
		"name":    identity.toolName,
	}
	missing := make(map[string]string, len(fields))
	observed := map[string]string{
		"id":      item.ID,
		"call_id": item.CallID,
		"name":    item.Name,
	}
	for field, value := range fields {
		if strings.TrimSpace(observed[field]) == "" && value != "" {
			missing[field] = value
		}
	}
	if len(missing) == 0 {
		return raw, nil
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, canonical.InternalError("responses terminal output item is invalid JSON")
	}
	for field, value := range missing {
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, canonical.InternalError("responses terminal output identity is invalid")
		}
		object[field] = encoded
	}
	restored, err := json.Marshal(object)
	if err != nil {
		return nil, canonical.InternalError("responses terminal output identity is invalid")
	}
	return restored, nil
}

func (s *responsesResponseStream) validatePendingTerminal(ctx context.Context, index int, terminal json.RawMessage, responseStatus string) error {
	slot := s.outputAt(index)
	var pendingItem responsesWireOutputItemDTO
	if err := json.Unmarshal(slot.terminalPendingItem, &pendingItem); err != nil {
		return canonical.InternalError("responses terminal-dependent item is invalid JSON")
	}
	pending, err := restoreResponsesTerminalIdentity(slot.terminalPendingItem, pendingItem, slot.identity)
	if err != nil {
		return err
	}
	pendingItems, err := decodeCompletedResponsesItemSetAtIndexes(
		ctx, s.request, s.toolNames, []json.RawMessage{pending}, "", []int{index}, true,
		responseStatus, s.exchangeID, nil,
	)
	if err != nil {
		return err
	}
	terminalItems, err := decodeCompletedResponsesItemSetAtIndexes(
		ctx, s.request, s.toolNames, []json.RawMessage{terminal}, "", []int{index}, true,
		responseStatus, s.exchangeID, nil,
	)
	if err != nil {
		return err
	}
	if compareResponsesCanonicalItems(pendingItems, terminalItems) != "" {
		return canonical.NewBackendError("responses", 0, "responses terminal output contradicts deferred partial checkpoint", "")
	}
	return nil
}

func (s *responsesResponseStream) completeObservedOutput(ctx context.Context, index int, raw json.RawMessage, item responsesWireOutputItemDTO, responseStatus string) (bool, []canonical.CanonicalItem, error) {
	slot := s.outputAt(index)
	frame := responsesCompletedStreamFrame(index, raw, item)
	slot.checkpointStatus = responsesCompletedItemStatus(item.Status)
	progressive := false
	switch strings.TrimSpace(item.Type) {
	case "message":
		if slot.text != nil {
			progressive = true
			if _, err := s.completeMessageItem(frame); err != nil {
				return false, nil, err
			}
		}
	case "function_call", "custom_tool_call":
		if slot.tool != nil {
			progressive = true
			toolType := canonical.ToolTypeFunction
			if strings.TrimSpace(item.Type) == "custom_tool_call" {
				toolType = canonical.ToolTypeCustom
			} else {
				arguments, err := responsesTerminalFunctionInput(item.Arguments)
				if err != nil {
					return false, nil, canonical.InternalError("responses terminal tool arguments are invalid")
				}
				frame.Item.Arguments = arguments
			}
			if _, err := s.completeToolState(frame, toolType, false, true); err != nil {
				return false, nil, err
			}
		}
	case "reasoning":
		if slot.reasoning != nil {
			progressive = true
			if _, err := s.completeReasoningState(frame); err != nil {
				return false, nil, err
			}
		}
	}
	if progressive {
		s.checkpointOutput(&index)
		return false, nil, nil
	}
	projectionSink := s.changeLog
	if slot.erased && slot.erasureRecorded {
		projectionSink = nil
	}
	items, err := decodeCompletedResponsesItemSetAtIndexes(ctx, s.request, s.toolNames, []json.RawMessage{raw}, "", []int{index}, true, responseStatus, s.exchangeID, projectionSink)
	if err != nil {
		return false, nil, err
	}
	if len(items) > 0 {
		if err := s.enqueueCompletedOutputItems(&index, items); err != nil {
			return false, nil, err
		}
	} else {
		s.erasedOutput = true
		s.omitProviderOutput(&index)
	}
	s.checkpointOutput(&index)
	return len(items) > 0, items, nil
}

func (s *responsesResponseStream) settleTerminalOmissions(ctx context.Context, observed map[int]struct{}, responseStatus string) error {
	indexes := make([]int, 0, len(s.providerOutputs))
	for index := range s.providerOutputs {
		if _, ok := observed[index]; !ok {
			indexes = append(indexes, index)
		}
	}
	sort.Ints(indexes)
	for _, index := range indexes {
		slot := s.outputAt(index)
		switch slot.phase {
		case responsesOutputCheckpointed:
			slot.phase = responsesOutputSettled
		case responsesOutputAwaitingTerminal:
			var item responsesWireOutputItemDTO
			if err := json.Unmarshal(slot.terminalPendingItem, &item); err != nil {
				return canonical.InternalError("responses terminal-dependent item is invalid JSON")
			}
			if _, err := admitCompletedResponsesOutputItem(item, responseStatus); err != nil {
				return err
			}
			raw, err := restoreResponsesTerminalIdentity(slot.terminalPendingItem, item, slot.identity)
			if err != nil {
				return err
			}
			if err := json.Unmarshal(raw, &item); err != nil {
				return canonical.InternalError("responses terminal-dependent item is invalid JSON")
			}
			if _, _, err := s.completeObservedOutput(ctx, index, raw, item, responseStatus); err != nil {
				return err
			}
			slot.terminalPendingItem = nil
			slot.phase = responsesOutputSettled
		case responsesOutputSettled:
			continue
		case responsesOutputAccumulating:
			continue
		default:
			return canonical.InternalError("responses output slot has invalid phase")
		}
	}
	return nil
}

// compareResponsesCanonicalItems returns an empty string for equal canonical
// semantics or one content-free closed category for the first mismatch.
func compareResponsesCanonicalItems(left []canonical.CanonicalItem, right []canonical.CanonicalItem) string {
	if len(left) != len(right) {
		return "item_count"
	}
	for index := range left {
		if left[index].Kind() != right[index].Kind() {
			return "item_kind"
		}
		if left[index].Kind() == canonical.ItemKindReasoning {
			if mismatch := compareResponsesReasoningItems(left[index], right[index]); mismatch != "" {
				return mismatch
			}
			continue
		}
		if !reflect.DeepEqual(left[index], right[index]) {
			switch left[index].Kind() {
			case canonical.ItemKindMessage:
				return "message_content"
			case canonical.ItemKindToolDeclarations:
				return "tool_declarations"
			case canonical.ItemKindToolCall:
				return "tool_call"
			case canonical.ItemKindToolResult:
				return "tool_result"
			case canonical.ItemKindToolDiscoveryResult:
				return "tool_discovery_result"
			default:
				return "canonical_item"
			}
		}
	}
	return ""
}

func compareResponsesReasoningItems(left canonical.CanonicalItem, right canonical.CanonicalItem) string {
	leftReasoning, leftOK := left.Reasoning()
	rightReasoning, rightOK := right.Reasoning()
	if !leftOK || !rightOK {
		return "reasoning_shape"
	}
	leftParts := leftReasoning.Parts()
	rightParts := rightReasoning.Parts()
	if len(leftParts) != len(rightParts) {
		return "reasoning_part_count"
	}
	for index := range leftParts {
		if leftParts[index].Kind() != rightParts[index].Kind() {
			return "reasoning_part_kind"
		}
		if leftParts[index].Text() != rightParts[index].Text() {
			return "reasoning_part_content"
		}
	}
	leftReplay, leftReplayOK := leftReasoning.Opaque().Responses()
	rightReplay, rightReplayOK := rightReasoning.Opaque().Responses()
	if leftReplayOK != rightReplayOK {
		return "reasoning_replay_presence"
	}
	if !leftReplayOK {
		return ""
	}
	if leftReplay.EncryptedContent != rightReplay.EncryptedContent {
		return "reasoning_replay_content"
	}
	if (leftReplay.ItemID == "") != (rightReplay.ItemID == "") {
		return "reasoning_replay_id_presence"
	}
	if leftReplay.ItemID != rightReplay.ItemID {
		return "reasoning_replay_id"
	}
	return ""
}

func responsesTerminalSuffix(progressive string, terminal string) (string, error) {
	if progressive == terminal {
		return "", nil
	}
	if strings.HasPrefix(terminal, progressive) {
		return terminal[len(progressive):], nil
	}
	return "", canonical.NewBackendError("responses", 0, "responses progressive output contradicts terminal checkpoint", "")
}

func (s *responsesResponseStream) completeOpenTextFromTerminal(terminal string) error {
	outputIndex, state := s.onlyOpenText()
	if state == nil {
		return nil
	}
	if len(state.parts) != 1 {
		return canonical.NewBackendError("responses", 0, "responses aggregate output_text cannot checkpoint multiple content parts", "")
	}
	part := state.parts[0]
	if part == nil || state.partFrontier != 1 || !part.classified || part.erased || !part.emitted {
		return canonical.NewBackendError("responses", 0, "responses aggregate output_text cannot checkpoint unresolved content indexes", "")
	}
	suffix, err := responsesTerminalSuffix(part.text.String(), terminal)
	if err != nil {
		return err
	}
	if suffix != "" {
		s.enqueueTextDelta(outputIndex, state.ordinal, part.ordinal, suffix)
		part.text.WriteString(suffix)
	}
	item, err := canonical.NewMessageItem(canonical.MessageRoleAssistant, []canonical.MessagePart{canonical.NewTextMessagePart(terminal)})
	if err != nil {
		return canonical.InternalError("responses terminal output text is invalid")
	}
	s.commitOutputItem(outputIndex, state.ordinal, item)
	s.outputAt(*outputIndex).checkpointStatus = "completed"
	s.checkpointOutput(outputIndex)
	s.outputAt(*outputIndex).phase = responsesOutputSettled
	s.outputAt(*outputIndex).text = nil
	return nil
}

func (s *responsesResponseStream) hasOpenText() bool {
	for _, output := range s.providerOutputs {
		if output.text != nil {
			return true
		}
	}
	return false
}

func (s *responsesResponseStream) onlyOpenText() (*int, *responsesTextState) {
	var foundIndex *int
	var found *responsesTextState
	for index, output := range s.providerOutputs {
		if output.text == nil {
			continue
		}
		if found != nil {
			return nil, nil
		}
		value := index
		foundIndex = &value
		found = output.text
	}
	return foundIndex, found
}

func responsesCompletedStreamFrame(index int, raw json.RawMessage, item responsesWireOutputItemDTO) streamFrame {
	frame := streamFrame{OutputIndex: &index, RawItem: raw}
	frame.Item.ID = item.ID
	frame.Item.Type = item.Type
	frame.Item.CallID = item.CallID
	frame.Item.Name = item.Name
	frame.Item.Input = item.Input
	frame.Item.Status = item.Status
	frame.Item.Content = item.Content
	frame.Item.Summary = item.Summary
	frame.Item.EncryptedContent = item.EncryptedContent
	frame.Item.Action = item.Action
	return frame
}

func responsesTerminalFunctionInput(raw json.RawMessage) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return "", canonical.InternalError("responses terminal tool arguments are missing")
	}
	input := string(trimmed)
	if trimmed[0] == '"' {
		if err := json.Unmarshal(trimmed, &input); err != nil {
			return "", err
		}
	}
	if _, err := canonical.ParseJSONObject([]byte(input)); err != nil {
		return "", err
	}
	return input, nil
}

func (s *responsesResponseStream) shiftPendingEvent() canonical.Event {
	event := s.pending[0]
	s.pending = s.pending[1:]
	return event
}
