package responses

import (
	"fmt"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

type streamFrame struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	Model     string `json:"model"`
	Delta     string `json:"delta"`
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
		ID     string `json:"id"`
		Type   string `json:"type"`
		CallID string `json:"call_id"`
		Name   string `json:"name"`
	} `json:"item"`
}

func (s *responsesEventReader) handleFrame(frame streamFrame) (bool, canonical.Event, error) {
	frameType := strings.TrimSpace(frame.Type) // swobu:io-string source=provider-wire
	switch frameType {
	case "response.created":
		s.handleResponseCreated(frame)
		return true, s.shiftPendingEvent(), nil
	case "response.output_text.delta":
		s.handleOutputTextDelta(frame)
		return true, s.shiftPendingEvent(), nil
	case "response.function_call_arguments.delta":
		s.handleFunctionCallArgumentsDelta(frame)
		return true, s.shiftPendingEvent(), nil
	case "response.output_item.added":
		if !s.handleOutputItemAdded(frame) {
			return false, canonical.Event{}, nil
		}
		return true, s.shiftPendingEvent(), nil
	case "response.function_call_arguments.done":
		s.handleFunctionCallArgumentsDone(frame)
		return true, s.shiftPendingEvent(), nil
	case "response.completed":
		s.handleResponseCompleted(frame)
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

func (s *responsesEventReader) handleFunctionCallArgumentsDelta(frame streamFrame) {
	itemID := fallbackItemID(frame.ItemID, frame.CallID)
	if !s.startedTool[itemID] {
		s.startedTool[itemID] = true
		toolEnvID := canonical.EnvelopeID(fmt.Sprintf("%s:item:%s", s.responseID, itemID))
		s.toolEnvIDs[itemID] = toolEnvID
		s.enqueueEnvelopeStart(toolEnvID, s.responseID, canonical.EnvelopeStartPayload{
			Kind:      canonical.EnvToolCall,
			Name:      frame.Name,
			ToolUseID: frame.CallID,
		}, canonical.EventMetadataFields{NativeID: itemID})
	}
	s.enqueueArgsDelta(s.toolEnvIDs[itemID], frame.Delta)
}

func (s *responsesEventReader) handleOutputItemAdded(frame streamFrame) bool {
	if strings.TrimSpace(frame.Item.Type) != "function_call" { // swobu:io-string source=boundary
		return false
	}
	itemID := fallbackItemID(frame.Item.ID, frame.Item.CallID)
	if s.startedTool[itemID] {
		return false
	}
	s.startedTool[itemID] = true
	toolEnvID := canonical.EnvelopeID(fmt.Sprintf("%s:item:%s", s.responseID, itemID))
	s.toolEnvIDs[itemID] = toolEnvID
	s.enqueueEnvelopeStart(toolEnvID, s.responseID, canonical.EnvelopeStartPayload{
		Kind:      canonical.EnvToolCall,
		Name:      frame.Item.Name,
		ToolUseID: frame.Item.CallID,
	}, canonical.EventMetadataFields{NativeID: itemID})
	return true
}

func (s *responsesEventReader) handleFunctionCallArgumentsDone(frame streamFrame) {
	itemID := fallbackItemID(frame.ItemID, frame.CallID)
	toolEnvID := s.toolEnvIDs[itemID]
	if toolEnvID != "" {
		s.enqueueEnvelopeEnd(toolEnvID, canonical.EnvToolCall, canonical.EnvelopeStatusCompleted)
	}
	delete(s.startedTool, itemID)
	delete(s.toolEnvIDs, itemID)
}

func (s *responsesEventReader) handleResponseCompleted(frame streamFrame) {
	s.completed = true
	status := strings.TrimSpace(frame.Status) // swobu:io-string source=boundary
	if status == "" {
		status = strings.TrimSpace(frame.Response.Status) // swobu:io-string source=boundary
	}
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
