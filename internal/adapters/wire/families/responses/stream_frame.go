package responses

import (
	"context"
	"fmt"
	"strings"

	deliverycompat "github.com/swobuforge/swobu/internal/adapters/wire/families/deliverycompat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

type streamFrame struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	Model     string `json:"model"`
	Delta     string `json:"delta"`
	Input     string `json:"input"`
	Status    string `json:"status"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	ItemID    string `json:"item_id"`
	Arguments string `json:"arguments"`
	Response  struct {
		ID     string `json:"id"`
		Model  string `json:"model"`
		Status string `json:"status"`
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
		s.handleResponseCreated(frame)
		return true, s.shiftPendingEvent(), nil
	case "response.output_text.delta":
		s.handleOutputTextDelta(frame)
		return true, s.shiftPendingEvent(), nil
	case "response.function_call_arguments.delta":
		s.handleToolArgumentsDelta(frame, canonical.ToolTypeFunction)
		return true, s.shiftPendingEvent(), nil
	case "response.mcp_call_arguments.delta":
		s.handleToolArgumentsDelta(frame, canonical.ToolTypeFunction)
		return true, s.shiftPendingEvent(), nil
	case "response.custom_tool_call_input.delta":
		s.handleToolArgumentsDelta(frame, canonical.ToolTypeCustom)
		return true, s.shiftPendingEvent(), nil
	case "response.output_item.added":
		if !s.handleOutputItemAdded(frame) {
			return false, canonical.Event{}, nil
		}
		return true, s.shiftPendingEvent(), nil
	case "response.function_call_arguments.done":
		s.handleToolArgumentsDone(frame, canonical.ToolTypeFunction)
		return true, s.shiftPendingEvent(), nil
	case "response.mcp_call_arguments.done":
		s.handleToolArgumentsDone(frame, canonical.ToolTypeFunction)
		return true, s.shiftPendingEvent(), nil
	case "response.custom_tool_call_input.done":
		s.handleToolArgumentsDone(frame, canonical.ToolTypeCustom)
		return true, s.shiftPendingEvent(), nil
	case "response.output_item.done":
		if !s.handleOutputItemDone(frame) {
			return false, canonical.Event{}, nil
		}
		return true, s.shiftPendingEvent(), nil
	case "response.completed":
		s.handleResponseCompleted(ctx, frame)
		return true, s.shiftPendingEvent(), nil
	case "error":
		return false, canonical.Event{}, canonical.InternalError("responses stream returned an error event")
	default:
		return false, canonical.Event{}, nil
	}
}

func (s *responsesEventReader) handleResponseCreated(frame streamFrame) {
	if !s.started {
		s.started = true
		s.enqueueEnvelopeStart(s.responseID, "", canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse})
	}
	resultID := strings.TrimSpace(frame.ID) // swobu:io-string source=boundary
	if resultID == "" {
		resultID = strings.TrimSpace(frame.Response.ID) // swobu:io-string source=boundary
	}
	model := strings.TrimSpace(frame.Model) // swobu:io-string source=boundary
	if model == "" {
		model = strings.TrimSpace(frame.Response.Model) // swobu:io-string source=boundary
	}
	s.enqueueMetadata(map[string]string{"result_id": resultID, "model": model})
}

func (s *responsesEventReader) handleOutputTextDelta(frame streamFrame) {
	if !s.textOpen {
		s.textOpen = true
		s.textEnvID = canonical.EnvelopeID(fmt.Sprintf("%s:item:text_0", s.responseID))
		s.enqueueEnvelopeStart(s.textEnvID, s.responseID, canonical.EnvelopeStartPayload{
			Kind: canonical.EnvMessage,
			Role: canonical.ItemAuthorAssistant,
		}, canonical.EventMetadataFields{NativeID: "text_0"})
	}
	s.enqueueTextDelta(s.textEnvID, frame.Delta)
}

func (s *responsesEventReader) handleToolArgumentsDelta(frame streamFrame, toolType string) {
	itemID := fallbackItemID(frame.ItemID, frame.CallID)
	s.ensureToolState(itemID, toolType, frame.CallID, frame.Name)
	s.enqueueToolArgs(itemID, frame.Delta)
}

func (s *responsesEventReader) handleOutputItemAdded(frame streamFrame) bool {
	itemType := strings.TrimSpace(frame.Item.Type) // swobu:io-string source=boundary
	var toolType string
	switch itemType {
	case "function_call", "mcp_call":
		toolType = canonical.ToolTypeFunction
	case "custom_tool_call":
		toolType = canonical.ToolTypeCustom
	default:
		return false
	}
	itemID := fallbackItemID(frame.Item.ID, frame.Item.CallID)
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

func (s *responsesEventReader) handleToolArgumentsDone(frame streamFrame, toolType string) {
	itemID := fallbackItemID(frame.ItemID, frame.CallID)
	state := s.ensureToolState(itemID, toolType, frame.CallID, frame.Name)
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
	}
	s.enqueueEnvelopeEnd(state.envID, canonical.EnvToolCall, canonical.EnvelopeStatusCompleted)
	delete(s.toolStates, itemID)
	delete(s.toolInputs, itemID)
}

func (s *responsesEventReader) handleOutputItemDone(frame streamFrame) bool {
	itemType := strings.TrimSpace(frame.Item.Type) // swobu:io-string source=boundary
	switch itemType {
	case "function_call", "mcp_call":
		s.handleToolArgumentsDone(frame, canonical.ToolTypeFunction)
		return true
	case "custom_tool_call":
		s.handleToolArgumentsDone(frame, canonical.ToolTypeCustom)
		return true
	default:
		return false
	}
}

func (s *responsesEventReader) handleResponseCompleted(ctx context.Context, frame streamFrame) {
	s.completed = true
	status := strings.TrimSpace(frame.Status) // swobu:io-string source=boundary
	if status == "" {
		status = strings.TrimSpace(frame.Response.Status) // swobu:io-string source=boundary
	}
	deliverycompat.EmitTerminalEventDecision(ctx, s.sink, s.exchangeID, !s.latestUsage.IsZero())
	s.closeOpenText(canonical.EnvelopeStatusCompleted)
	s.closeOpenTools(canonical.EnvelopeStatusCompleted)
	s.enqueueUsage(s.latestUsage)
	s.enqueueFinish(status)
	s.enqueueEnvelopeEnd(s.responseID, canonical.EnvResponse, canonical.EnvelopeStatusCompleted)
}

func (s *responsesEventReader) shiftPendingEvent() canonical.Event {
	event := s.pending[0]
	s.pending = s.pending[1:]
	return event
}
