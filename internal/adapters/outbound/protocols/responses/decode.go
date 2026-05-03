// event-state machine together so migration behavior stays recoverable.
package responses

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/swobuforge/swobu/internal/adapters/outbound/protocols"
	"github.com/swobuforge/swobu/internal/domain/compatibility"
)

type responseEnvelope struct {
	ID         string `json:"id"`
	Model      string `json:"model"`
	OutputText string `json:"output_text"`
	Output     []struct {
		Type      string          `json:"type"`
		Role      string          `json:"role"`
		Content   json.RawMessage `json:"content"`
		CallID    string          `json:"call_id"`
		Name      string          `json:"name"`
		Arguments string          `json:"arguments"`
	} `json:"output"`
}

var tokenUsagePathSpec = protocols.TokenUsagePathSpec{
	InputPaths: [][]string{
		{"usage", "input_tokens"},
		{"usage", "prompt_tokens"},
		{"response", "usage", "input_tokens"},
		{"response", "usage", "prompt_tokens"},
		{"usageMetadata", "promptTokenCount"},
		{"usage", "inputTokens"},
		{"response", "usage", "inputTokens"},
	},
	OutputPaths: [][]string{
		{"usage", "output_tokens"},
		{"usage", "completion_tokens"},
		{"response", "usage", "output_tokens"},
		{"response", "usage", "completion_tokens"},
		{"usageMetadata", "candidatesTokenCount"},
		{"usage", "outputTokens"},
		{"response", "usage", "outputTokens"},
	},
	CacheReadPaths: [][]string{
		{"usage", "input_tokens_details", "cached_tokens"},
		{"usage", "prompt_tokens_details", "cached_tokens"},
		{"response", "usage", "input_tokens_details", "cached_tokens"},
		{"response", "usage", "prompt_tokens_details", "cached_tokens"},
		{"usage", "cache_read_input_tokens"},
		{"response", "usage", "cache_read_input_tokens"},
		{"usageMetadata", "cachedContentTokenCount"},
		{"usage", "cacheReadInputTokens"},
		{"response", "usage", "cacheReadInputTokens"},
	},
	CacheWritePaths: [][]string{
		{"usage", "input_tokens_details", "cache_write_tokens"},
		{"usage", "prompt_tokens_details", "cache_write_tokens"},
		{"response", "usage", "input_tokens_details", "cache_write_tokens"},
		{"response", "usage", "prompt_tokens_details", "cache_write_tokens"},
		{"usage", "cache_creation_input_tokens"},
		{"response", "usage", "cache_creation_input_tokens"},
		{"usage", "cacheWriteInputTokens"},
		{"response", "usage", "cacheWriteInputTokens"},
	},
}

func DecodeBufferedResult(raw []byte) (compatibility.CanonicalOutputValue, error) {
	var dto responseEnvelope
	if err := json.Unmarshal(raw, &dto); err != nil {
		return compatibility.CanonicalOutputValue{}, compatibility.InternalError("responses output is invalid JSON")
	}
	items, err := decodeOutputItems(dto.Output, dto.OutputText)
	if err != nil {
		return compatibility.CanonicalOutputValue{}, err
	}
	return compatibility.NewConversationOutputWithUsage(
		dto.ID,
		dto.Model,
		items,
		"completed",
		protocols.ExtractTokenUsage(raw, tokenUsagePathSpec),
	), nil
}

func DecodeStream(body io.ReadCloser) compatibility.CanonicalOutputEventStream {
	return &canonicalOutputEventStreamCloser{
		reader:      protocols.NewSSEReader(body),
		startedTool: map[string]bool{},
		textOpen:    false,
		latestUsage: compatibility.NewUnknownTokenUsage(),
	}
}

type canonicalOutputEventStreamCloser struct {
	reader      *protocols.SSEReaderCloser
	pending     []compatibility.OutputEvent
	startedTool map[string]bool
	textOpen    bool
	latestUsage compatibility.TokenUsage
}

// ordered state machine over text, tool calls, reasoning, and terminal frames.
// variants while maintaining canonical output ordering.
func (s *canonicalOutputEventStreamCloser) Next() (compatibility.OutputEvent, error) {
	if len(s.pending) > 0 {
		event := s.pending[0]
		s.pending = s.pending[1:]
		return event, nil
	}
	for {
		event, err := s.reader.Next()
		if err != nil {
			return compatibility.OutputEvent{}, err
		}
		if strings.TrimSpace(event.Data) == "[DONE]" {
			continue
		}
		rawFrame := []byte(event.Data)
		frameUsage := protocols.ExtractTokenUsage(rawFrame, tokenUsagePathSpec)
		if !frameUsage.IsZero() {
			s.latestUsage = frameUsage
		}
		var frame struct {
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
		if err := json.Unmarshal(rawFrame, &frame); err != nil {
			return compatibility.OutputEvent{}, compatibility.InternalError("responses stream event is invalid JSON")
		}
		switch strings.TrimSpace(frame.Type) {
		case "response.created":
			resultID := strings.TrimSpace(frame.ID)
			if resultID == "" {
				resultID = strings.TrimSpace(frame.Response.ID)
			}
			model := strings.TrimSpace(frame.Model)
			if model == "" {
				model = strings.TrimSpace(frame.Response.Model)
			}
			return compatibility.OutputEvent{
				Kind:     compatibility.OutputEventStarted,
				ResultID: resultID,
				Model:    model,
			}, nil
		case "response.output_text.delta":
			if !s.textOpen {
				s.textOpen = true
				s.pending = append(s.pending, compatibility.OutputEvent{
					Kind:     compatibility.OutputEventItemStarted,
					ItemKind: compatibility.OutputItemText,
					ItemID:   "text_0",
				})
			}
			s.pending = append(s.pending, compatibility.OutputEvent{
				Kind:      compatibility.OutputEventTextDelta,
				ItemID:    "text_0",
				TextDelta: frame.Delta,
			})
			event := s.pending[0]
			s.pending = s.pending[1:]
			return event, nil
		case "response.function_call_arguments.delta":
			itemID := fallbackItemID(frame.ItemID, frame.CallID)
			if !s.startedTool[itemID] {
				s.startedTool[itemID] = true
				s.pending = append(s.pending, compatibility.OutputEvent{
					Kind:      compatibility.OutputEventItemStarted,
					ItemKind:  compatibility.OutputItemToolUse,
					ItemID:    itemID,
					ToolUseID: frame.CallID,
					Name:      frame.Name,
				})
			}
			s.pending = append(s.pending, compatibility.OutputEvent{
				Kind:           compatibility.OutputEventToolUseArgumentsDelta,
				ItemKind:       compatibility.OutputItemToolUse,
				ItemID:         itemID,
				ToolUseID:      frame.CallID,
				Name:           frame.Name,
				ArgumentsDelta: frame.Delta,
			})
			event := s.pending[0]
			s.pending = s.pending[1:]
			return event, nil
		case "response.output_item.added":
			if strings.TrimSpace(frame.Item.Type) != "function_call" {
				continue
			}
			itemID := fallbackItemID(frame.Item.ID, frame.Item.CallID)
			if s.startedTool[itemID] {
				continue
			}
			s.startedTool[itemID] = true
			return compatibility.OutputEvent{
				Kind:      compatibility.OutputEventItemStarted,
				ItemKind:  compatibility.OutputItemToolUse,
				ItemID:    itemID,
				ToolUseID: frame.Item.CallID,
				Name:      frame.Item.Name,
			}, nil
		case "response.function_call_arguments.done":
			itemID := fallbackItemID(frame.ItemID, frame.CallID)
			s.pending = append(s.pending, compatibility.OutputEvent{
				Kind:      compatibility.OutputEventItemCompleted,
				ItemKind:  compatibility.OutputItemToolUse,
				ItemID:    itemID,
				ToolUseID: frame.CallID,
				Name:      frame.Name,
			})
			delete(s.startedTool, itemID)
			event := s.pending[0]
			s.pending = s.pending[1:]
			return event, nil
		case "response.completed":
			status := strings.TrimSpace(frame.Status)
			if status == "" {
				status = strings.TrimSpace(frame.Response.Status)
			}
			if s.textOpen {
				s.pending = append(s.pending, compatibility.OutputEvent{
					Kind:     compatibility.OutputEventItemCompleted,
					ItemKind: compatibility.OutputItemText,
					ItemID:   "text_0",
				})
				s.textOpen = false
			}
			s.pending = append(s.pending, compatibility.OutputEvent{
				Kind:         compatibility.OutputEventCompleted,
				FinishReason: status,
				Usage:        s.latestUsage,
			})
			event := s.pending[0]
			s.pending = s.pending[1:]
			return event, nil
		case "error":
			return compatibility.OutputEvent{}, compatibility.InternalError("responses stream returned an error event")
		default:
			continue
		}
	}
}

func (s *canonicalOutputEventStreamCloser) Close() error {
	return s.reader.Close()
}

func fallbackItemID(itemID string, callID string) string {
	if strings.TrimSpace(itemID) != "" {
		return strings.TrimSpace(itemID)
	}
	if strings.TrimSpace(callID) != "" {
		return strings.TrimSpace(callID)
	}
	return "tool_0"
}

func decodeOutputItems(items []struct {
	Type      string          `json:"type"`
	Role      string          `json:"role"`
	Content   json.RawMessage `json:"content"`
	CallID    string          `json:"call_id"`
	Name      string          `json:"name"`
	Arguments string          `json:"arguments"`
}, outputText string) ([]compatibility.OutputItem, error) {
	output := make([]compatibility.OutputItem, 0, len(items))
	for _, item := range items {
		switch strings.TrimSpace(item.Type) {
		case "message":
			parts, err := decodeMessageTextParts(item.Content)
			if err != nil {
				return nil, err
			}
			for idx, part := range parts {
				output = append(output, compatibility.NewTextOutputItem(fmt.Sprintf("text_%d", len(output)+idx), part.Text))
			}
		case "function_call":
			input := map[string]any{}
			if strings.TrimSpace(item.Arguments) != "" {
				if err := json.Unmarshal([]byte(item.Arguments), &input); err != nil {
					return nil, compatibility.InternalError("responses function_call arguments are invalid")
				}
			}
			itemID := fallbackItemID("", item.CallID)
			output = append(output, compatibility.NewToolUseOutputItem(itemID, strings.TrimSpace(item.CallID), strings.TrimSpace(item.Name), input))
		case "reasoning":
			continue
		default:
			return nil, compatibility.UnsupportedOperation("responses output item type is not implemented")
		}
	}
	if len(output) == 0 && strings.TrimSpace(outputText) != "" {
		output = append(output, compatibility.NewTextOutputItem("text_0", outputText))
	}
	return output, nil
}

func decodeMessageTextParts(raw json.RawMessage) ([]compatibility.CanonicalItem, error) {
	var content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &content); err != nil {
		return nil, compatibility.InternalError("responses message content is invalid")
	}
	parts := make([]compatibility.CanonicalItem, 0, len(content))
	for _, part := range content {
		partType := strings.TrimSpace(part.Type)
		switch partType {
		case "text", "output_text", "input_text":
			parts = append(parts, compatibility.NewTextItem(compatibility.ItemAuthorAssistant, part.Text))
		default:
			return nil, compatibility.UnsupportedOperation("responses message content part type is not implemented")
		}
	}
	return parts, nil
}
