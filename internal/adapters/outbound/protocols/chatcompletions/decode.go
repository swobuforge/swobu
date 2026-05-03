// event-state machine together so migration behavior stays recoverable.
package chatcompletions

import (
	"encoding/json"
	"io"
	"strconv"
	"strings"

	"github.com/swobuforge/swobu/internal/adapters/outbound/protocols"
	"github.com/swobuforge/swobu/internal/domain/compatibility"
)

type responseBody struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Role      string          `json:"role"`
			Content   json.RawMessage `json:"content"`
			ToolCalls []toolCallBody  `json:"tool_calls"`
		} `json:"message"`
		Delta struct {
			Role      string               `json:"role"`
			Content   string               `json:"content"`
			ToolCalls []streamToolCallBody `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
}

type streamToolCallBody struct {
	Index    int                    `json:"index"`
	ID       string                 `json:"id,omitempty"`
	Type     string                 `json:"type,omitempty"`
	Function streamToolFunctionBody `json:"function"`
}

type streamToolFunctionBody struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

var tokenUsagePathSpec = protocols.TokenUsagePathSpec{
	InputPaths: [][]string{
		{"usage", "prompt_tokens"},
		{"usage", "input_tokens"},
		{"usageMetadata", "promptTokenCount"},
		{"usage", "inputTokens"},
	},
	OutputPaths: [][]string{
		{"usage", "completion_tokens"},
		{"usage", "output_tokens"},
		{"usageMetadata", "candidatesTokenCount"},
		{"usage", "outputTokens"},
	},
	CacheReadPaths: [][]string{
		{"usage", "prompt_tokens_details", "cached_tokens"},
		{"usage", "input_tokens_details", "cached_tokens"},
		{"usage", "cache_read_input_tokens"},
		{"usageMetadata", "cachedContentTokenCount"},
		{"usage", "cacheReadInputTokens"},
	},
	CacheWritePaths: [][]string{
		{"usage", "prompt_tokens_details", "cache_write_tokens"},
		{"usage", "input_tokens_details", "cache_write_tokens"},
		{"usage", "cache_creation_input_tokens"},
		{"usage", "cacheWriteInputTokens"},
	},
}

func DecodeBufferedResult(raw []byte) (compatibility.CanonicalOutputValue, error) {
	var dto responseBody
	if err := json.Unmarshal(raw, &dto); err != nil {
		return compatibility.CanonicalOutputValue{}, compatibility.InternalError("chat completions response is invalid JSON")
	}
	if len(dto.Choices) == 0 {
		return compatibility.CanonicalOutputValue{}, compatibility.InternalError("chat completions response is missing choices")
	}
	choice := dto.Choices[0]
	items, err := decodeResponseOutputItems(choice.Message.Content, choice.Message.ToolCalls)
	if err != nil {
		return compatibility.CanonicalOutputValue{}, err
	}
	return compatibility.NewConversationOutputWithUsage(
		dto.ID,
		dto.Model,
		items,
		choice.FinishReason,
		protocols.ExtractTokenUsage(raw, tokenUsagePathSpec),
	), nil
}

func DecodeStream(body io.ReadCloser) compatibility.CanonicalOutputEventStream {
	return newStreamDecoder(body)
}

type canonicalOutputEventStreamCloser struct {
	reader      *protocols.SSEReaderCloser
	started     bool
	resultID    string
	model       string
	completed   bool
	pending     []compatibility.OutputEvent
	textOpen    bool
	toolCalls   map[int]streamToolState
	latestUsage compatibility.TokenUsage
}

type streamToolState struct {
	OutputItemID string
	ToolUseID    string
	Name         string
	Started      bool
	PendingArgs  []string
}

func newStreamDecoder(body io.ReadCloser) *canonicalOutputEventStreamCloser {
	return &canonicalOutputEventStreamCloser{
		reader:      protocols.NewSSEReader(body),
		toolCalls:   map[int]streamToolState{},
		latestUsage: compatibility.NewUnknownTokenUsage(),
	}
}

// ordered state machine over text, tool calls, and terminal frames.
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
		rawChunk := []byte(event.Data)
		chunkUsage := protocols.ExtractTokenUsage(rawChunk, tokenUsagePathSpec)
		if !chunkUsage.IsZero() {
			s.latestUsage = chunkUsage
		}
		var chunk responseBody
		if err := json.Unmarshal(rawChunk, &chunk); err != nil {
			return compatibility.OutputEvent{}, compatibility.InternalError("chat completions stream chunk is invalid JSON")
		}
		if !s.started {
			s.started = true
			s.resultID = chunk.ID
			s.model = chunk.Model
			s.enqueue(compatibility.OutputEvent{
				Kind:     compatibility.OutputEventStarted,
				ResultID: chunk.ID,
				Model:    chunk.Model,
			})
		}
		if len(chunk.Choices) == 0 {
			if len(s.pending) > 0 {
				return s.shiftPending(), nil
			}
			continue
		}
		choice := chunk.Choices[0]
		if choice.Delta.Content != "" {
			if !s.textOpen {
				s.textOpen = true
				s.enqueue(compatibility.OutputEvent{
					Kind:     compatibility.OutputEventItemStarted,
					ItemKind: compatibility.OutputItemText,
					ResultID: s.resultID,
					Model:    s.model,
					ItemID:   "text_0",
				})
			}
			s.enqueue(compatibility.OutputEvent{
				Kind:      compatibility.OutputEventTextDelta,
				ResultID:  s.resultID,
				Model:     s.model,
				ItemID:    "text_0",
				TextDelta: choice.Delta.Content,
			})
		}
		for _, call := range choice.Delta.ToolCalls {
			if err := s.queueToolCallDelta(call); err != nil {
				return compatibility.OutputEvent{}, err
			}
		}
		if strings.TrimSpace(choice.FinishReason) != "" && !s.completed {
			if s.textOpen {
				s.enqueue(compatibility.OutputEvent{
					Kind:     compatibility.OutputEventItemCompleted,
					ItemKind: compatibility.OutputItemText,
					ResultID: s.resultID,
					Model:    s.model,
					ItemID:   "text_0",
				})
				s.textOpen = false
			}
			for idx, state := range s.toolCalls {
				if state.Started {
					s.enqueue(compatibility.OutputEvent{
						Kind:      compatibility.OutputEventItemCompleted,
						ItemKind:  compatibility.OutputItemToolUse,
						ResultID:  s.resultID,
						Model:     s.model,
						ItemID:    state.OutputItemID,
						ToolUseID: state.ToolUseID,
						Name:      state.Name,
					})
				}
				delete(s.toolCalls, idx)
			}
			s.completed = true
			s.enqueue(compatibility.OutputEvent{
				Kind:         compatibility.OutputEventCompleted,
				ResultID:     s.resultID,
				Model:        s.model,
				FinishReason: choice.FinishReason,
				Usage:        s.latestUsage,
			})
		}
		if len(s.pending) > 0 {
			return s.shiftPending(), nil
		}
	}
}

func (s *canonicalOutputEventStreamCloser) Close() error {
	return s.reader.Close()
}

func (s *canonicalOutputEventStreamCloser) enqueue(event compatibility.OutputEvent) {
	s.pending = append(s.pending, event)
}

func (s *canonicalOutputEventStreamCloser) shiftPending() compatibility.OutputEvent {
	event := s.pending[0]
	s.pending = s.pending[1:]
	return event
}

func (s *canonicalOutputEventStreamCloser) queueToolCallDelta(call streamToolCallBody) error {
	state := s.toolCalls[call.Index]
	if state.OutputItemID == "" {
		state.OutputItemID = "tool_" + strconv.Itoa(call.Index)
	}
	if state.ToolUseID == "" {
		state.ToolUseID = strings.TrimSpace(call.ID)
		if state.ToolUseID == "" {
			state.ToolUseID = "toolu_swobu_" + strconv.Itoa(call.Index)
		}
	}
	if strings.TrimSpace(call.Function.Name) != "" {
		state.Name = strings.TrimSpace(call.Function.Name)
	}
	if call.Function.Arguments != "" {
		state.PendingArgs = append(state.PendingArgs, call.Function.Arguments)
	}
	if !state.Started && strings.TrimSpace(state.Name) == "" {
		s.toolCalls[call.Index] = state
		return nil
	}
	if !state.Started {
		state.Started = true
		s.enqueue(compatibility.OutputEvent{
			Kind:      compatibility.OutputEventItemStarted,
			ItemKind:  compatibility.OutputItemToolUse,
			ResultID:  s.resultID,
			Model:     s.model,
			ItemID:    state.OutputItemID,
			ToolUseID: state.ToolUseID,
			Name:      state.Name,
		})
	}
	for _, delta := range state.PendingArgs {
		s.enqueue(compatibility.OutputEvent{
			Kind:           compatibility.OutputEventToolUseArgumentsDelta,
			ItemKind:       compatibility.OutputItemToolUse,
			ResultID:       s.resultID,
			Model:          s.model,
			ItemID:         state.OutputItemID,
			ToolUseID:      state.ToolUseID,
			Name:           state.Name,
			ArgumentsDelta: delta,
		})
	}
	state.PendingArgs = nil
	s.toolCalls[call.Index] = state
	return nil
}

func decodeResponseOutputItems(content json.RawMessage, toolCalls []toolCallBody) ([]compatibility.OutputItem, error) {
	items, err := decodeOpenAIContentItems(content)
	if err != nil {
		return nil, compatibility.InternalError("chat completions response content is unsupported")
	}
	out := make([]compatibility.OutputItem, 0, len(items)+len(toolCalls))
	for idx, item := range items {
		if item.Kind != compatibility.ItemKindText {
			continue
		}
		out = append(out, compatibility.NewTextOutputItem("text_"+strconv.Itoa(idx), item.Text))
	}
	for _, call := range toolCalls {
		if call.Type != "" && call.Type != "function" {
			return nil, compatibility.InternalError("chat completions response tool call type is unsupported")
		}
		input := map[string]any{}
		if strings.TrimSpace(call.Function.Arguments) != "" {
			if err := json.Unmarshal([]byte(call.Function.Arguments), &input); err != nil {
				return nil, compatibility.InternalError("chat completions response tool call arguments are invalid")
			}
		}
		itemID := strings.TrimSpace(call.ID)
		if itemID == "" {
			itemID = "tool_0"
		}
		out = append(out, compatibility.NewToolUseOutputItem(itemID, strings.TrimSpace(call.ID), strings.TrimSpace(call.Function.Name), input))
	}
	return out, nil
}

func decodeOpenAIContentItems(raw json.RawMessage) ([]compatibility.CanonicalItem, error) {
	if len(strings.TrimSpace(string(raw))) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		if text == "" {
			return nil, nil
		}
		return []compatibility.CanonicalItem{compatibility.NewTextItem(compatibility.ItemAuthorAssistant, text)}, nil
	}

	var parts []struct {
		Type       string `json:"type"`
		Text       string `json:"text"`
		InputText  string `json:"input_text"`
		OutputText string `json:"output_text"`
	}
	if err := json.Unmarshal(raw, &parts); err != nil {
		return nil, err
	}
	decoded := make([]compatibility.CanonicalItem, 0, len(parts))
	for _, part := range parts {
		partType := strings.TrimSpace(part.Type)
		if partType == "" {
			partType = "text"
		}
		switch partType {
		case "text", "input_text", "output_text":
			text := part.Text
			if text == "" {
				text = part.InputText
			}
			if text == "" {
				text = part.OutputText
			}
			if text != "" {
				decoded = append(decoded, compatibility.NewTextItem(compatibility.ItemAuthorAssistant, text))
			}
		default:
			return nil, compatibility.UnsupportedOperation("chat completions response content contains an unsupported part type")
		}
	}
	return decoded, nil
}
