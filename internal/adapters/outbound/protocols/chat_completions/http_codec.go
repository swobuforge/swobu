// translation in one place so request and stream semantics stay recoverable.
package chatcompletions

import (
	"encoding/json"
	"strings"

	httpcodec "github.com/swobuforge/swobu/internal/adapters/outbound/protocols/httpcodec"
	openaicompat "github.com/swobuforge/swobu/internal/adapters/outbound/protocols/openaicompat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

type ChatCompletionsFamilyCodec struct{}

func (ChatCompletionsFamilyCodec) DecodeRequest(raw []byte) (canonical.CanonicalRequest, bool, error) {
	var dto chatCompletionsRequestDTO
	if err := json.Unmarshal(raw, &dto); err != nil {
		return nil, false, canonical.BadRequest("chat completions request body is invalid JSON")
	}
	if len(dto.Messages) == 0 {
		return nil, false, canonical.BadRequest("chat completions request is missing required fields")
	}
	items := make([]canonical.CanonicalItem, 0, len(dto.Messages))
	for idx, msg := range dto.Messages {
		decoded, err := decodeChatCompletionsItems(msg.Role, msg.Content, msg.ToolCalls, msg.ToolCallID, idx)
		if err != nil {
			return nil, false, err
		}
		items = append(items, decoded...)
	}
	return canonical.NewDialogRequest(strings.TrimSpace(dto.Model), items), dto.Stream, nil // swobu:io-string source=boundary
}

func (ChatCompletionsFamilyCodec) EncodeBuffered(output canonical.CanonicalOutput) ([]byte, error) {
	return json.Marshal(chatCompletionsResponseDTO{
		ID:     httpcodec.FallbackID(output.ResultID(), "chatcmpl_swobu"),
		Object: "chat.completion",
		Model:  output.Model(),
		Choices: []chatCompletionsChoiceDTO{{
			Index:        0,
			Message:      chatMessageFromOutput(output),
			FinishReason: httpcodec.DefaultFinishReason(output.FinishReason(), "stop"),
		}},
		Usage: chatUsageFromCanonical(output.Usage()),
	})
}

func (ChatCompletionsFamilyCodec) NewStreamState() httpcodec.EnvelopeStreamEncoder {
	return &chatCompletionsEnvelopeStreamEncoder{adapter: httpcodec.NewEnvelopeEventAdapter()}
}

func decodeChatCompletionsItems(
	role string,
	content json.RawMessage,
	toolCalls []chatCompletionsToolCallDTO,
	toolCallID string,
	msgIdx int,
) ([]canonical.CanonicalItem, error) {
	author := openaicompat.AuthorForRole(role)
	textItems, err := openaicompat.DecodeTextContentItems(content, "chat completions", author)
	if err != nil {
		return nil, err
	}

	role = strings.TrimSpace(role) // swobu:io-string source=boundary
	if role == "tool" {
		if strings.TrimSpace(toolCallID) == "" { // swobu:io-string source=boundary
			return nil, canonical.BadRequest("chat completions tool messages require tool_call_id")
		}
		return []canonical.CanonicalItem{
			canonical.NewToolResultItem(canonical.ItemAuthorTool, strings.TrimSpace(toolCallID), joinItemText(textItems)), // swobu:io-string source=boundary
		}, nil
	}

	items := append([]canonical.CanonicalItem(nil), textItems...)
	for idx, call := range toolCalls {
		if call.Type != "" && call.Type != "function" {
			return nil, canonical.BadRequest("chat completions request contains an unsupported tool call type")
		}
		if strings.TrimSpace(call.Function.Name) == "" { // swobu:io-string source=boundary
			return nil, canonical.BadRequest("chat completions tool calls require a function name")
		}
		input, err := httpcodec.DecodeJSONObject(call.Function.Arguments, "chat completions tool call arguments are invalid")
		if err != nil {
			return nil, err
		}
		id := strings.TrimSpace(call.ID) // swobu:io-string source=boundary
		if id == "" {
			id = openaicompat.GeneratedToolUseID(msgIdx, idx)
		}
		items = append(items, canonical.NewToolUseItem(author, "", id, strings.TrimSpace(call.Function.Name), input)) // swobu:io-string source=boundary
	}
	return items, nil
}

func joinItemText(items []canonical.CanonicalItem) string {
	var builder strings.Builder
	for _, item := range items {
		if item.Kind != canonical.ItemKindText {
			continue
		}
		builder.WriteString(item.Text)
	}
	return builder.String()
}

type chatCompletionsEnvelopeStreamEncoder struct {
	resultID string
	model    string
	started  bool
	toolByID map[string]int
	adapter  *httpcodec.EnvelopeEventAdapter
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
func (s *chatCompletionsEnvelopeStreamEncoder) Encode(event httpcodec.StreamEvent) ([][]byte, error) {
	if s.toolByID == nil {
		s.toolByID = map[string]int{}
	}
	switch event.Kind {
	case httpcodec.StreamEventStarted:
		s.resultID = httpcodec.FallbackID(event.ResultID, "chatcmpl_swobu")
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
		return [][]byte{httpcodec.SSEData(raw)}, nil
	case httpcodec.StreamEventItemStarted:
		if event.ItemKind == canonical.ItemKindToolUse {
			index := len(s.toolByID)
			s.toolByID[event.ItemID] = index
			raw, _ := json.Marshal(chatCompletionsResponseDTO{
				ID:     httpcodec.FallbackID(s.resultID, "chatcmpl_swobu"),
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
			return [][]byte{httpcodec.SSEData(raw)}, nil
		}
		return nil, nil
	case httpcodec.StreamEventTextDelta:
		if !s.started {
			frames, _ := s.Encode(httpcodec.StreamEvent{Kind: httpcodec.StreamEventStarted, ResultID: s.resultID, Model: s.model})
			more, err := s.Encode(event)
			return append(frames, more...), err
		}
		raw, _ := json.Marshal(chatCompletionsResponseDTO{
			ID:     httpcodec.FallbackID(s.resultID, "chatcmpl_swobu"),
			Object: "chat.completion.chunk",
			Model:  s.model,
			Choices: []chatCompletionsChoiceDTO{{
				Index: 0,
				Delta: &chatCompletionsDeltaDTO{Content: event.TextDelta},
			}},
		})
		return [][]byte{httpcodec.SSEData(raw)}, nil
	case httpcodec.StreamEventToolUseArgumentsDelta:
		index, ok := s.toolByID[event.ItemID]
		if !ok {
			startFrames, err := s.Encode(httpcodec.StreamEvent{
				Kind:      httpcodec.StreamEventItemStarted,
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
			ID:     httpcodec.FallbackID(s.resultID, "chatcmpl_swobu"),
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
		return [][]byte{httpcodec.SSEData(raw)}, nil
	case httpcodec.StreamEventItemCompleted:
		return nil, nil
	case httpcodec.StreamEventCompleted:
		raw, _ := json.Marshal(chatCompletionsResponseDTO{
			ID:     httpcodec.FallbackID(s.resultID, "chatcmpl_swobu"),
			Object: "chat.completion.chunk",
			Model:  s.model,
			Choices: []chatCompletionsChoiceDTO{{
				Index:        0,
				Delta:        &chatCompletionsDeltaDTO{},
				FinishReason: httpcodec.DefaultFinishReason(event.FinishReason, "stop"),
			}},
			Usage: chatUsageFromCanonical(event.Usage),
		})
		return [][]byte{
			httpcodec.SSEData(raw),
			[]byte("data: [DONE]\n\n"),
		}, nil
	default:
		return nil, canonical.UnsupportedOperation("chat completions streaming event is not implemented")
	}
}

func (s *chatCompletionsEnvelopeStreamEncoder) Finish() ([][]byte, error) { return nil, nil }

func chatMessageFromOutput(output canonical.CanonicalOutput) chatCompletionsResponseMessageDTO {
	text := httpcodec.OutputText(output.Items())
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
