// translation in one place so request and stream semantics stay recoverable.
package chatcompletions

import (
	"encoding/json"

	sse "github.com/swobuforge/swobu/internal/adapters/wire/framing/sse"
	"github.com/swobuforge/swobu/internal/domain/canonical"
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
	default:
		return nil, canonical.UnsupportedOperation("chat completions streaming event is not implemented")
	}
}

func (s *chatCompletionsEnvelopeStreamEncoder) Finish() ([][]byte, error) { return nil, nil }

func chatMessageFromOutput(output canonical.CanonicalOutput) chatCompletionsResponseMessageDTO {
	text := sse.OutputText(output.Items())
	message := chatCompletionsResponseMessageDTO{
		Role: "assistant",
	}
	if text != "" {
		message.Content = text
	}
	if toolCalls := chatToolCalls(output.Items()); len(toolCalls) > 0 {
		message.ToolCalls = toolCalls
	}
	return message
}

func chatToolCalls(items []canonical.OutputItem) []chatCompletionsResponseToolCallDTO {
	out := make([]chatCompletionsResponseToolCallDTO, 0)
	for _, item := range items {
		if item.Kind != canonical.ItemKindToolUse {
			continue
		}
		args, _ := json.Marshal(item.Input)
		out = append(out, chatCompletionsResponseToolCallDTO{
			ID:   item.ToolUseID,
			Type: "function",
			Function: chatCompletionsResponseFunctionDTO{
				Name:      item.Name,
				Arguments: string(args),
			},
		})
	}
	return out
}

func chatUsageFromCanonical(usage canonical.TokenUsage) *chatCompletionsUsageDTO {
	input, hasInput := usage.InputTokens()
	output, hasOutput := usage.OutputTokens()
	cacheRead, hasCacheRead := usage.CacheReadTokens()
	cacheWrite, hasCacheWrite := usage.CacheWriteTokens()
	if !hasInput && !hasOutput && !hasCacheRead && !hasCacheWrite {
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
	return dto
}
