package responses

import (
	"context"
	"fmt"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	deliverycompat "github.com/swobuforge/swobu/internal/wire/deliverycompat"
)

type streamFrame struct {
	Type        string `json:"type"`
	ID          string `json:"id"`
	Model       string `json:"model"`
	Delta       string `json:"delta"`
	Input       string `json:"input"`
	Status      string `json:"status"`
	CallID      string `json:"call_id"`
	Name        string `json:"name"`
	ItemID      string `json:"item_id"`
	OutputIndex *int   `json:"output_index"`
	Arguments   string `json:"arguments"`
	Response    struct {
		ID                string                         `json:"id"`
		Model             string                         `json:"model"`
		Status            string                         `json:"status"`
		IncompleteDetails *responsesIncompleteDetailsDTO `json:"incomplete_details,omitempty"`
		ContentFilters    []responsesContentFilterDTO    `json:"content_filters,omitempty"`
		Output            []responsesWireOutputItemDTO   `json:"output,omitempty"`
		OutputText        string                         `json:"output_text,omitempty"`
	} `json:"response"`
	Item struct {
		ID          string `json:"id"`
		Type        string `json:"type"`
		CallID      string `json:"call_id"`
		Name        string `json:"name"`
		Arguments   string `json:"arguments"`
		Input       string `json:"input"`
		ServerLabel string `json:"server_label"`
	} `json:"item"`
}

func (s *responsesEventReader) handleFrame(ctx context.Context, frame streamFrame) (bool, canonical.Event, error) {
	frameType := strings.TrimSpace(frame.Type) // swobu:io-string source=provider-wire
	switch frameType {
	case "response.created":
		if err := s.handleResponseCreated(ctx, frame); err != nil {
			return false, canonical.Event{}, err
		}
		return true, canonical.Event{}, nil
	case "response.output_text.delta":
		s.handleOutputTextDelta(frame)
		return true, canonical.Event{}, nil
	case "response.function_call_arguments.delta":
		s.handleToolArgumentsDelta(frame, canonical.ToolTypeFunction)
		return true, canonical.Event{}, nil
	case "response.mcp_call_arguments.delta":
		s.handleToolArgumentsDelta(frame, canonical.ToolTypeFunction)
		return true, canonical.Event{}, nil
	case "response.custom_tool_call_input.delta":
		s.handleToolArgumentsDelta(frame, canonical.ToolTypeCustom)
		return true, canonical.Event{}, nil
	case "response.output_item.added":
		if !s.handleOutputItemAdded(frame) {
			return false, canonical.Event{}, nil
		}
		return true, canonical.Event{}, nil
	case "response.function_call_arguments.done":
		s.handleToolArgumentsDone(frame, canonical.ToolTypeFunction)
		return true, canonical.Event{}, nil
	case "response.mcp_call_arguments.done":
		s.handleToolArgumentsDone(frame, canonical.ToolTypeFunction)
		return true, canonical.Event{}, nil
	case "response.custom_tool_call_input.done":
		s.handleToolArgumentsDone(frame, canonical.ToolTypeCustom)
		return true, canonical.Event{}, nil
	case "response.output_item.done":
		if !s.handleOutputItemDone(frame) {
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

func (s *responsesEventReader) handleResponseCreated(ctx context.Context, frame streamFrame) error {
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
	s.enqueueEnvelopeStart(s.responseEnvID, "", canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse, Response: canonical.ResponseRef{Responses: &canonical.ResponsesNativeRef{ProviderResponseID: canonical.NewResponsesNativeResponseID(providerResponseID)}}}, canonical.EventMetadataFields{})
	model := strings.TrimSpace(frame.Model) // swobu:io-string source=boundary
	if model == "" {
		model = strings.TrimSpace(frame.Response.Model) // swobu:io-string source=boundary
	}
	s.enqueueMetadata(map[string]string{"model": model})
	return nil
}

func (s *responsesEventReader) handleOutputTextDelta(frame streamFrame) {
	s.emittedOutput = true
	if !s.textOpen {
		s.textOpen = true
		s.textEnvID = canonical.EnvelopeID(fmt.Sprintf("%s:item:text_0", s.responseEnvID))
		s.enqueueEnvelopeStart(s.textEnvID, s.responseEnvID, canonical.EnvelopeStartPayload{
			Kind: canonical.EnvMessage,
			Role: canonical.ItemAuthorAssistant,
		}, canonical.EventMetadataFields{NativeID: "text_0"})
	}
	s.enqueueTextDelta(s.textEnvID, frame.Delta)
}

func (s *responsesEventReader) handleToolArgumentsDelta(frame streamFrame, toolType string) {
	s.emittedOutput = true
	itemID := fallbackItemID(frame.ItemID, frame.CallID, frame.OutputIndex)
	s.ensureToolState(itemID, toolType, frame.CallID, frame.Name)
	s.enqueueToolArgs(itemID, frame.Delta)
}

func (s *responsesEventReader) handleOutputItemAdded(frame streamFrame) bool {
	itemType := strings.TrimSpace(frame.Item.Type) // swobu:io-string source=boundary
	var toolType string
	switch itemType {
	case "function_call", "mcp_call":
		s.emittedOutput = true
		toolType = canonical.ToolTypeFunction
	case "custom_tool_call":
		s.emittedOutput = true
		toolType = canonical.ToolTypeCustom
	default:
		return false
	}
	itemID := fallbackItemID(frame.Item.ID, frame.Item.CallID, frame.OutputIndex)
	if _, ok := s.toolStates[itemID]; ok {
		return false
	}
	state := s.ensureToolState(itemID, toolType, frame.Item.CallID, frame.Item.Name)
	initial := ""
	if toolType == canonical.ToolTypeCustom {
		initial = frame.Item.Input
	} else {
		initial = frame.Item.Arguments
	}
	if initial != "" {
		s.enqueueArgsDelta(state.envID, initial)
		s.toolInputs[itemID] += initial
	}
	return true
}

func (s *responsesEventReader) handleToolArgumentsDone(frame streamFrame, toolType string) bool {
	return s.completeToolState(frame, toolType, true, false)
}

func (s *responsesEventReader) handleOutputItemDone(frame streamFrame) bool {
	itemType := strings.TrimSpace(frame.Item.Type) // swobu:io-string source=boundary
	switch itemType {
	case "function_call", "mcp_call":
		return s.completeToolState(frame, canonical.ToolTypeFunction, false, true)
	case "custom_tool_call":
		return s.completeToolState(frame, canonical.ToolTypeCustom, false, true)
	default:
		return false
	}
}

func (s *responsesEventReader) completeToolState(frame streamFrame, toolType string, argumentsDone bool, outputDone bool) bool {
	s.emittedOutput = true
	itemID := fallbackItemID(frame.ItemID, frame.CallID, frame.OutputIndex)
	state := s.ensureToolState(itemID, toolType, frame.CallID, frame.Name)
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
		s.enqueueArgsDelta(state.envID, finalInput)
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
		return emitted
	}
	s.enqueueEnvelopeEnd(state.envID, canonical.EnvToolCall, canonical.EnvelopeStatusCompleted)
	emitted = true
	if toolType == canonical.ToolTypeCustom {
		delete(s.toolStates, itemID)
		delete(s.toolInputs, itemID)
		return emitted
	}
	state.closed = true
	s.toolStates[itemID] = state
	return emitted
}

func (s *responsesEventReader) handleResponseTerminal(ctx context.Context, frame streamFrame) error {
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
		fallbackItems := []canonical.OutputItem(nil)
		if !s.emittedOutput {
			items, err := decodeOutputItems(ctx, frame.Response.Output, frame.Response.OutputText, s.exchangeID, s.sink)
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

func (s *responsesEventReader) shiftPendingEvent() canonical.Event {
	event := s.pending[0]
	s.pending = s.pending[1:]
	return event
}

func (s *responsesEventReader) enqueueCompletedOutputItems(items []canonical.OutputItem) error {
	textIdx := 0
	toolIdx := 0
	for _, item := range items {
		switch item.Kind() {
		case canonical.ItemKindText:
			text, ok := item.TextItem()
			if !ok {
				return canonical.InternalError("responses text item payload is invalid")
			}
			envID := canonical.EnvelopeID(fmt.Sprintf("%s:item:text_%d", s.responseEnvID, textIdx))
			metaID := fmt.Sprintf("text_%d", textIdx)
			textIdx++
			s.enqueueEnvelopeStart(envID, s.responseEnvID, canonical.EnvelopeStartPayload{
				Kind: canonical.EnvMessage,
				Role: canonical.ItemAuthorAssistant,
			}, canonical.EventMetadataFields{NativeID: metaID})
			s.enqueue(canonical.Event{Kind: canonical.EventTextDelta, EnvID: envID, Payload: canonical.TextDeltaPayload{Text: text.Text}})
			s.enqueueEnvelopeEnd(envID, canonical.EnvMessage, canonical.EnvelopeStatusCompleted)
		case canonical.ItemKindToolUse:
			toolUse, ok := item.ToolUse()
			if !ok {
				return canonical.InternalError("responses tool-use item payload is invalid")
			}
			envID := canonical.EnvelopeID(fmt.Sprintf("%s:item:tool_%d", s.responseEnvID, toolIdx))
			toolIdx++
			toolUseID := toolUse.UseID
			if toolUseID == "" {
				toolUseID = item.ItemID()
			}
			s.enqueueEnvelopeStart(envID, s.responseEnvID, canonical.EnvelopeStartPayload{
				Kind:      canonical.EnvToolCall,
				Name:      toolUse.Name,
				ToolUseID: toolUseID,
				ToolType:  toolUse.ToolType,
			}, canonical.EventMetadataFields{NativeID: item.ItemID()})
			args := toolUse.Input.RawObject()
			if args != "" {
				s.enqueue(canonical.Event{Kind: canonical.EventArgsDelta, EnvID: envID, Payload: canonical.ArgsDeltaPayload{Args: args}})
			}
			s.enqueueEnvelopeEnd(envID, canonical.EnvToolCall, canonical.EnvelopeStatusCompleted)
		}
	}
	return nil
}
