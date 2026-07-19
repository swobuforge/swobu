// translation in one place so request and stream semantics stay recoverable.
package chatcompletions

import (
	"encoding/json"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	sse "github.com/swobuforge/swobu/internal/wire/framing/sse"
)

type chatCompletionsEnvelopeStreamEncoder struct {
	resultID string
	model    string
	started  bool
	toolByID map[string]int
	adapter  *sse.EnvelopeEventAdapter
}

func (s *chatCompletionsEnvelopeStreamEncoder) EncodeEnvelopeEvent(event canonical.Event) ([][]byte, error) {
	streamEvents := s.adapter.Translate(event)
	frames := make([][]byte, 0, len(streamEvents))
	for _, streamEvent := range streamEvents {
		emitted, err := s.Encode(streamEvent)
		if err != nil {
			return nil, err
		}
		frames = append(frames, emitted...)
	}
	return frames, nil
}

// event-to-frame fanout over text, tool calls, and terminal envelopes.
func (s *chatCompletionsEnvelopeStreamEncoder) Encode(event sse.StreamEvent) ([][]byte, error) {
	if s.toolByID == nil {
		s.toolByID = map[string]int{}
	}
	switch event.Kind {
	case sse.StreamEventStarted:
		s.resultID = sse.FallbackID(event.ResultID, "chatcmpl_swobu")
		s.model = event.Model
		s.started = true
		raw, _ := json.Marshal(chatCompletionsResponseDTO{
			ID:     s.resultID,
			Object: "chat.completion.chunk",
			Model:  s.model,
			Choices: []chatCompletionsChoiceDTO{{
				Index: 0,
				Delta: &chatCompletionsDeltaDTO{Role: "assistant"},
			}},
		})
		return [][]byte{sse.SSEData(raw)}, nil
	case sse.StreamEventItemStarted:
		if event.ItemKind == canonical.ItemKindToolUse {
			if strings.ToLower(strings.TrimSpace(event.ToolType)) == canonical.ToolTypeCustom { // swobu:io-string source=domain
				return nil, canonical.UnsupportedOperation("chat completions streaming does not support custom tool calls")
			}
			index := len(s.toolByID)
			s.toolByID[event.ItemID] = index
			raw, _ := json.Marshal(chatCompletionsResponseDTO{
				ID:     sse.FallbackID(s.resultID, "chatcmpl_swobu"),
				Object: "chat.completion.chunk",
				Model:  s.model,
				Choices: []chatCompletionsChoiceDTO{{
					Index: 0,
					Delta: &chatCompletionsDeltaDTO{
						ToolCalls: []chatCompletionsDeltaToolCallDTO{{
							Index: index,
							ID:    event.ToolUseID,
							Type:  "function",
							Function: chatCompletionsDeltaFunctionDTO{
								Name:      event.Name,
								Arguments: "",
							},
						}},
					},
				}},
			})
			return [][]byte{sse.SSEData(raw)}, nil
		}
		return nil, nil
	case sse.StreamEventTextDelta:
		if !s.started {
			frames, _ := s.Encode(sse.StreamEvent{Kind: sse.StreamEventStarted, ResultID: s.resultID, Model: s.model})
			more, err := s.Encode(event)
			return append(frames, more...), err
		}
		raw, _ := json.Marshal(chatCompletionsResponseDTO{
			ID:     sse.FallbackID(s.resultID, "chatcmpl_swobu"),
			Object: "chat.completion.chunk",
			Model:  s.model,
			Choices: []chatCompletionsChoiceDTO{{
				Index: 0,
				Delta: &chatCompletionsDeltaDTO{Content: event.TextDelta},
			}},
		})
		return [][]byte{sse.SSEData(raw)}, nil
	case sse.StreamEventToolUseArgumentsDelta:
		if strings.ToLower(strings.TrimSpace(event.ToolType)) == canonical.ToolTypeCustom { // swobu:io-string source=domain
			return nil, canonical.UnsupportedOperation("chat completions streaming does not support custom tool calls")
		}
		index, ok := s.toolByID[event.ItemID]
		if !ok {
			startFrames, err := s.Encode(sse.StreamEvent{
				Kind:      sse.StreamEventItemStarted,
				ItemKind:  canonical.ItemKindToolUse,
				ResultID:  s.resultID,
				Model:     s.model,
				ItemID:    event.ItemID,
				ToolUseID: event.ToolUseID,
				Name:      event.Name,
			})
			if err != nil {
				return nil, err
			}
			frame, err := s.Encode(event)
			return append(startFrames, frame...), err
		}
		raw, _ := json.Marshal(chatCompletionsResponseDTO{
			ID:     sse.FallbackID(s.resultID, "chatcmpl_swobu"),
			Object: "chat.completion.chunk",
			Model:  s.model,
			Choices: []chatCompletionsChoiceDTO{{
				Index: 0,
				Delta: &chatCompletionsDeltaDTO{
					ToolCalls: []chatCompletionsDeltaToolCallDTO{{
						Index: index,
						Function: chatCompletionsDeltaFunctionDTO{
							Arguments: event.ArgumentsDelta,
						},
					}},
				},
			}},
		})
		return [][]byte{sse.SSEData(raw)}, nil
	case sse.StreamEventItemCompleted:
		return nil, nil
	case sse.StreamEventCompleted:
		raw, _ := json.Marshal(chatCompletionsResponseDTO{
			ID:     sse.FallbackID(s.resultID, "chatcmpl_swobu"),
			Object: "chat.completion.chunk",
			Model:  s.model,
			Choices: []chatCompletionsChoiceDTO{{
				Index:        0,
				Delta:        &chatCompletionsDeltaDTO{},
				FinishReason: sse.DefaultFinishReason(event.FinishReason, "stop"),
			}},
			Usage: chatUsageFromCanonical(event.Usage),
		})
		return [][]byte{
			sse.SSEData(raw),
			[]byte("data: [DONE]\n\n"),
		}, nil
	case sse.StreamEventFailed:
		raw, _ := json.Marshal(map[string]any{
			"error": map[string]any{
				"message": sse.FallbackID(event.ErrorMessage, "output stream failed"),
				"type":    "swobu_stream_error",
				"code":    sse.FallbackID(event.ErrorCode, "stream_error"),
			},
		})
		return [][]byte{
			sse.SSEData(raw),
			[]byte("data: [DONE]\n\n"),
		}, nil
	default:
		return nil, canonical.UnsupportedOperation("chat completions streaming event is not implemented")
	}
}

func (s *chatCompletionsEnvelopeStreamEncoder) Finish() ([][]byte, error) { return nil, nil }

func chatMessageFromOutput(output canonical.CanonicalOutput) (chatCompletionsResponseMessageDTO, error) {
	message := chatCompletionsResponseMessageDTO{
		Role: "assistant",
	}
	var text strings.Builder
	toolCalls := make([]chatCompletionsResponseToolCallDTO, 0)
	for _, item := range output.Items() {
		switch item.Kind() {
		case canonical.ItemKindText:
			textItem, ok := item.TextItem()
			if !ok {
				return chatCompletionsResponseMessageDTO{}, canonical.InternalError("chat completions text item payload is invalid")
			}
			text.WriteString(textItem.Text)
		case canonical.ItemKindToolUse:
			wire, err := chatToolCallFromOutputItem(item)
			if err != nil {
				return chatCompletionsResponseMessageDTO{}, err
			}
			toolCalls = append(toolCalls, wire)
		default:
			return chatCompletionsResponseMessageDTO{}, canonical.UnsupportedOperation("chat completions protocol only supports text and tool use output items")
		}
	}
	if content := text.String(); content != "" {
		message.Content = content
	}
	if len(toolCalls) > 0 {
		message.ToolCalls = toolCalls
	}
	return message, nil
}

// swobu:lint ignore string-switch because=protocol boundary encodes canonical tool-call kinds.
func chatToolCallFromOutputItem(item canonical.OutputItem) (chatCompletionsResponseToolCallDTO, error) {
	toolUse, ok := item.ToolUse()
	if !ok {
		return chatCompletionsResponseToolCallDTO{}, canonical.InternalError("chat completions tool-use item payload is invalid")
	}
	toolUseID := strings.TrimSpace(toolUse.UseID) // swobu:io-string source=boundary
	if toolUseID == "" {
		return chatCompletionsResponseToolCallDTO{}, canonical.BadRequest("chat completions response tool calls require tool_use_id")
	}
	name := strings.TrimSpace(toolUse.Name) // swobu:io-string source=boundary
	if name == "" {
		return chatCompletionsResponseToolCallDTO{}, canonical.BadRequest("chat completions response tool calls require a name")
	}
	args := toolUse.Input.RawObject()
	switch strings.ToLower(strings.TrimSpace(toolUse.ToolType)) { // swobu:io-string source=domain
	case "", canonical.ToolTypeFunction:
		return chatCompletionsResponseToolCallDTO{
			ID:   toolUseID,
			Type: "function",
			Function: &chatCompletionsResponseFunctionDTO{
				Name:      name,
				Arguments: args,
			},
		}, nil
	case canonical.ToolTypeCustom:
		return chatCompletionsResponseToolCallDTO{
			ID:   toolUseID,
			Type: "custom",
			Custom: &chatCompletionsResponseCustomDTO{
				Name:  name,
				Input: args,
			},
		}, nil
	default:
		return chatCompletionsResponseToolCallDTO{}, canonical.UnsupportedOperation("chat completions protocol only supports function and custom tool output items")
	}
}

func chatUsageFromCanonical(usage canonical.TokenUsage) *chatCompletionsUsageDTO {
	input, hasInput := usage.InputTokens()
	output, hasOutput := usage.OutputTokens()
	reasoning, hasReasoning := usage.ReasoningTokens()
	cacheRead, hasCacheRead := usage.CacheReadTokens()
	cacheWrite, hasCacheWrite := usage.CacheWriteTokens()
	if !hasInput && !hasOutput && !hasReasoning && !hasCacheRead && !hasCacheWrite {
		return nil
	}
	dto := &chatCompletionsUsageDTO{
		PromptTokens:     input,
		CompletionTokens: output,
		TotalTokens:      input + output,
	}
	if hasCacheRead || hasCacheWrite {
		dto.PromptDetails = &chatCompletionsPromptTokenDetailsDTO{
			CachedTokens:     cacheRead,
			CacheWriteTokens: cacheWrite,
		}
	}
	if hasReasoning {
		// Preserve provider-reported reasoning usage as a separate accounting
		// fact; do not fold it into total_tokens or drop a zero value.
		dto.CompletionDetails = &chatCompletionsCompletionTokenDetailsDTO{
			ReasoningTokens: reasoning,
		}
	}
	return dto
}
