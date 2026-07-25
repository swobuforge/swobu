package responses

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	deliverycompat "github.com/swobuforge/swobu/internal/wire/deliverycompat"
)

type streamFrame struct {
	RawItem      json.RawMessage   `json:"-"`
	RawOutput    []json.RawMessage `json:"-"`
	Type         string            `json:"type"`
	ID           string            `json:"id"`
	Model        string            `json:"model"`
	Delta        string            `json:"delta"`
	Input        string            `json:"input"`
	Status       string            `json:"status"`
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
		IncompleteDetails *responsesIncompleteDetailsDTO `json:"incomplete_details,omitempty"`
		ContentFilters    []responsesContentFilterDTO    `json:"content_filters,omitempty"`
		Output            []json.RawMessage              `json:"output,omitempty"`
		OutputText        string                         `json:"output_text,omitempty"`
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
		return true, canonical.Event{}, nil
	case "response.custom_tool_call_input.delta":
		if err := s.handleToolArgumentsDelta(frame, canonical.ToolTypeCustom); err != nil {
			return false, canonical.Event{}, err
		}
		return true, canonical.Event{}, nil
	case "response.output_item.added":
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
		return true, canonical.Event{}, nil
	case "response.custom_tool_call_input.done":
		if _, err := s.handleToolArgumentsDone(frame, canonical.ToolTypeCustom); err != nil {
			return false, canonical.Event{}, err
		}
		return true, canonical.Event{}, nil
	case "response.output_item.done":
		handled, err := s.handleOutputItemDone(frame)
		if err != nil {
			return false, canonical.Event{}, err
		}
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
	case "error":
		return false, canonical.Event{}, canonical.InternalError("responses stream returned an error event")
	default:
		return false, canonical.Event{}, nil
	}
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
	emitNativeResponseIDCaptured(ctx, s.sink, s.exchangeID, providerResponseID)
	model := strings.TrimSpace(frame.Model) // swobu:io-string source=boundary
	if model == "" {
		model = strings.TrimSpace(frame.Response.Model) // swobu:io-string source=boundary
	}
	s.enqueueEnvelopeStart(s.responseEnvID, "", canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse, Model: model}, canonical.EventMetadataFields{})
	s.enqueue(canonical.Event{Kind: canonical.EventResponseIdentity, EnvID: s.responseEnvID, Payload: canonical.ResponseIdentityPayload{Response: canonical.ResponseRef{Responses: &canonical.ResponsesContinuation{ProviderResponseID: canonical.NewResponsesResponseID(providerResponseID)}}}})
	return nil
}

func (s *responsesResponseStream) handleOutputTextDelta(frame streamFrame) error {
	s.emittedOutput = true
	if s.textState != nil && !s.textState.accepts(frame.OutputIndex) {
		s.closeOpenText(canonical.EnvelopeStatusCompleted)
	}
	if s.textState == nil {
		start, err := canonical.NewMessageStart(canonical.MessageRoleAssistant)
		if err != nil {
			return err
		}
		ordinal := s.ordinalFor("text", frame.OutputIndex)
		envID := canonical.EnvelopeID(fmt.Sprintf("%s:item:%d", s.responseEnvID, ordinal))
		s.textState = newResponsesTextState(envID, ordinal, frame.OutputIndex)
		s.enqueueItemStart(envID, ordinal, start)
		s.enqueue(canonical.Event{Kind: canonical.EventContentStart, Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: ordinal}, Payload: canonical.NewMessageContentStart(canonical.PartKindText)}})
	}
	s.textState.text.WriteString(frame.Delta)
	s.enqueueTextDelta(s.textState.envID, s.textState.ordinal, frame.Delta)
	return nil
}

func (s *responsesResponseStream) handleToolArgumentsDelta(frame streamFrame, toolType string) error {
	s.emittedOutput = true
	s.closeOpenText(canonical.EnvelopeStatusCompleted)
	itemID := frame.ItemID
	if itemID == "" {
		itemID = frame.Item.ID
	}
	itemID = fallbackItemID(itemID, frame.CallID, frame.OutputIndex)
	ordinal := s.ordinalFor(itemID, frame.OutputIndex)
	if _, err := s.ensureToolState(itemID, ordinal, toolType, frame.CallID, frame.Name); err != nil {
		return err
	}
	s.enqueueToolArgs(itemID, frame.Delta)
	return nil
}

func (s *responsesResponseStream) handleOutputItemAdded(frame streamFrame) (bool, error) {
	itemType := strings.TrimSpace(frame.Item.Type) // swobu:io-string source=boundary
	if itemType == "reasoning" {
		s.closeOpenText(canonical.EnvelopeStatusCompleted)
		itemID := fallbackItemID(frame.Item.ID, "reasoning", frame.OutputIndex)
		if _, exists := s.reasoningStates[itemID]; exists {
			return false, nil
		}
		s.reasoningStates[itemID] = &responsesReasoningState{id: frame.Item.ID, status: frame.Item.Status}
		return true, nil
	}
	var toolType string
	switch itemType {
	case "function_call":
		s.emittedOutput = true
		toolType = canonical.ToolTypeFunction
	case "custom_tool_call":
		s.emittedOutput = true
		toolType = canonical.ToolTypeCustom
	case "message", "web_search_call":
		return false, nil
	default:
		s.omitProviderOutput(frame.OutputIndex)
		return false, nil
	}
	itemID := fallbackItemID(frame.Item.ID, frame.Item.CallID, frame.OutputIndex)
	s.closeOpenText(canonical.EnvelopeStatusCompleted)
	if _, ok := s.toolStates[itemID]; ok {
		return false, nil
	}
	ordinal := s.ordinalFor(itemID, frame.OutputIndex)
	state, err := s.ensureToolState(itemID, ordinal, toolType, frame.Item.CallID, frame.Item.Name)
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
		s.enqueueArgsDelta(state.envID, state.ordinal, initial)
		s.toolInputs[itemID] += initial
	}
	return true, nil
}

func (s *responsesResponseStream) handleToolArgumentsDone(frame streamFrame, toolType string) (bool, error) {
	return s.completeToolState(frame, toolType, true, false)
}

func (s *responsesResponseStream) handleOutputItemDone(frame streamFrame) (bool, error) {
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
				return false, canonical.InternalError("responses streamed actionless web-search marker is not completed")
			}
			s.omitProviderOutput(frame.OutputIndex)
			return true, nil
		}
		state, err := decodeResponsesWebSearchLifecycleState(frame.Item.Status)
		if err != nil {
			return false, canonical.NotImplemented("Swobu cannot project this valid streamed Responses web-search history state")
		}
		if state != responsesWebSearchSucceeded {
			return false, canonical.InternalError("responses completed web-search stream item is not terminal")
		}
		return s.completeWebSearchItem(frame)
	case "message":
		return s.completeMessageItem(frame)
	default:
		if len(frame.RawItem) == 0 {
			return false, canonical.InternalError("responses completed opaque item is missing")
		}
		items, err := decodeCompletedResponsesItem(s.request, frame.RawItem)
		if err != nil {
			return false, err
		}
		if err := s.enqueueCompletedOutputItems(items); err != nil {
			return false, err
		}
		if len(items) == 0 {
			s.omitProviderOutput(frame.OutputIndex)
		} else {
			s.emittedOutput = true
		}
		return true, nil
	}
}

func (s *responsesResponseStream) completeToolState(frame streamFrame, toolType string, argumentsDone bool, outputDone bool) (bool, error) {
	s.emittedOutput = true
	itemID := frame.ItemID
	if itemID == "" {
		itemID = frame.Item.ID
	}
	itemID = fallbackItemID(itemID, frame.CallID, frame.OutputIndex)
	callID := frame.CallID
	name := frame.Name
	if callID == "" {
		callID = frame.Item.CallID
	}
	if name == "" {
		name = frame.Item.Name
	}
	ordinal := s.ordinalFor(itemID, frame.OutputIndex)
	state, err := s.ensureToolState(itemID, ordinal, toolType, callID, name)
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
	if finalInput != "" && s.toolInputs[itemID] == "" {
		s.enqueueArgsDelta(state.envID, state.ordinal, finalInput)
		s.toolInputs[itemID] = finalInput
		emitted = true
	}
	if argumentsDone {
		state = s.markToolStateArgumentsDone(itemID, state)
	}
	if outputDone {
		state = s.markToolStateOutputDone(itemID, state)
	}
	if state.closed {
		if state.argumentsDone && state.outputDone {
			delete(s.toolStates, itemID)
			delete(s.toolInputs, itemID)
		} else {
			s.toolStates[itemID] = state
		}
		return emitted, nil
	}
	if outputDone {
		var input canonical.ToolInput
		if toolType == canonical.ToolTypeCustom {
			input = canonical.NewTextToolInput(s.toolInputs[itemID])
		} else {
			object, parseErr := canonical.ParseJSONObject([]byte(s.toolInputs[itemID]))
			if parseErr != nil {
				return false, canonical.InternalError("responses streamed tool arguments are invalid")
			}
			input = canonical.NewJSONObjectToolInput(object)
		}
		item, itemErr := canonical.NewToolCallItem(state.callID, state.tool, input)
		if itemErr != nil {
			return false, canonical.InternalError("responses streamed tool call is invalid")
		}
		s.enqueueItemCompleted(state.envID, state.ordinal, item)
		state.closed = true
		emitted = true
		delete(s.toolStates, itemID)
		delete(s.toolInputs, itemID)
		return emitted, nil
	}
	s.toolStates[itemID] = state
	return emitted, nil
}

func (s *responsesResponseStream) handleResponseTerminal(ctx context.Context, frame streamFrame) error {
	if !s.started {
		if err := s.handleResponseCreated(ctx, frame); err != nil {
			return err
		}
	}
	if terminalReason, promptBlocked := responsesTerminalReason(frame.Type, frame.Status, frame.Response.Status, frame.Response.ContentFilters, responseIncompleteReason(frame.Response.IncompleteDetails)); promptBlocked {
		s.completed = true
		deliverycompat.EmitTerminalUsagePresence(ctx, s.sink, s.exchangeID, !s.latestUsage.IsZero())
		s.closeOpenText(canonical.EnvelopeStatusError)
		s.closeOpenTools(canonical.EnvelopeStatusError)
		s.enqueueError("content_filter", responsesContentFilterMessage(responsesBlockedContentFilterSource(frame.Response.ContentFilters)))
		s.enqueueEnvelopeEnd(s.responseEnvID, canonical.EnvResponse, canonical.EnvelopeStatusError)
		return nil
	} else {
		usedFallback := false
		fallbackItems := []canonical.CanonicalItem(nil)
		if !s.emittedOutput {
			items, err := decodeOutputItems(ctx, s.request, frame.Response.Output, frame.Response.OutputText, s.exchangeID, s.sink)
			if err != nil {
				return err
			}
			if len(items) > 0 {
				if err := s.enqueueCompletedOutputItems(items); err != nil {
					return err
				}
				s.emittedOutput = true
				usedFallback = true
				fallbackItems = items
			}
		}
		s.completed = true
		deliverycompat.EmitTerminalUsagePresence(ctx, s.sink, s.exchangeID, !s.latestUsage.IsZero())
		s.closeOpenText(canonical.EnvelopeStatusCompleted)
		s.closeOpenTools(canonical.EnvelopeStatusCompleted)
		s.enqueueUsage(s.latestUsage)
		s.enqueueFinish(terminalReason)
		s.enqueueEnvelopeEnd(s.responseEnvID, canonical.EnvResponse, canonical.EnvelopeStatusCompleted)
		logResponsesTerminalProjection(usedFallback, terminalReason, len(frame.Response.Output), strings.TrimSpace(frame.Response.OutputText) != "", fallbackItems) // swobu:io-string source=provider-wire
		return nil
	}
}

func (s *responsesResponseStream) shiftPendingEvent() canonical.Event {
	event := s.pending[0]
	s.pending = s.pending[1:]
	return event
}
