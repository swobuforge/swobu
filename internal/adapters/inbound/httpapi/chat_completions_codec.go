// translation in one place so request and stream semantics stay recoverable.
package httpapi

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/metrofun/swobu/internal/domain/compatibility"
)

type chatCompletionsFamilyCodec struct{}

func (chatCompletionsFamilyCodec) decodeRequest(raw []byte) (compatibility.CanonicalRequest, compatibility.DeliveryMode, error) {
	var dto chatCompletionsRequestDTO
	if err := json.Unmarshal(raw, &dto); err != nil {
		return nil, "", compatibility.BadRequest("chat completions request body is invalid JSON")
	}
	if len(dto.Messages) == 0 {
		return nil, "", compatibility.BadRequest("chat completions request is missing required fields")
	}
	items := make([]compatibility.CanonicalItem, 0, len(dto.Messages))
	for idx, msg := range dto.Messages {
		decoded, err := decodeChatCompletionsItems(msg.Role, msg.Content, msg.ToolCalls, msg.ToolCallID, idx)
		if err != nil {
			return nil, "", err
		}
		items = append(items, decoded...)
	}
	return compatibility.NewDialogRequest(strings.TrimSpace(dto.Model), items), deliveryModeFromStream(dto.Stream), nil
}

func (chatCompletionsFamilyCodec) encodeBuffered(output compatibility.CanonicalOutput) ([]byte, error) {
	return json.Marshal(chatCompletionsResponseDTO{
		ID:     fallbackID(output.ResultID(), "chatcmpl_swobu"),
		Object: "chat.completion",
		Model:  output.Model(),
		Choices: []chatCompletionsChoiceDTO{{
			Index:        0,
			Message:      chatMessageFromOutput(output),
			FinishReason: defaultFinishReason(output.FinishReason(), "stop"),
		}},
		Usage: chatUsageFromCanonical(output.Usage()),
	})
}

func (chatCompletionsFamilyCodec) newStreamState() clientStreamEncoder {
	return &chatCompletionClientStreamEncoder{}
}

func decodeChatCompletionsItems(
	role string,
	content json.RawMessage,
	toolCalls []chatCompletionsToolCallDTO,
	toolCallID string,
	msgIdx int,
) ([]compatibility.CanonicalItem, error) {
	author := authorForRole(role)
	textItems, err := decodeTextContentItems(content, "chat completions", author)
	if err != nil {
		return nil, err
	}

	role = strings.TrimSpace(role)
	if role == "tool" {
		if strings.TrimSpace(toolCallID) == "" {
			return nil, compatibility.BadRequest("chat completions tool messages require tool_call_id")
		}
		return []compatibility.CanonicalItem{
			compatibility.NewToolResultItem(compatibility.ItemAuthorTool, strings.TrimSpace(toolCallID), joinItemText(textItems)),
		}, nil
	}

	items := append([]compatibility.CanonicalItem(nil), textItems...)
	for idx, call := range toolCalls {
		if call.Type != "" && call.Type != "function" {
			return nil, compatibility.BadRequest("chat completions request contains an unsupported tool call type")
		}
		if strings.TrimSpace(call.Function.Name) == "" {
			return nil, compatibility.BadRequest("chat completions tool calls require a function name")
		}
		input, err := decodeJSONObject(call.Function.Arguments, "chat completions tool call arguments are invalid")
		if err != nil {
			return nil, err
		}
		id := strings.TrimSpace(call.ID)
		if id == "" {
			id = generatedToolUseID(msgIdx, idx)
		}
		items = append(items, compatibility.NewToolUseItem(author, "", id, strings.TrimSpace(call.Function.Name), input))
	}
	return items, nil
}

func decodeTextContentItems(raw json.RawMessage, surface string, author compatibility.ItemAuthor) ([]compatibility.CanonicalItem, error) {
	if len(strings.TrimSpace(string(raw))) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		if text == "" {
			return nil, nil
		}
		return []compatibility.CanonicalItem{compatibility.NewTextItem(author, text)}, nil
	}

	var parts []textContentPartDTO
	if err := json.Unmarshal(raw, &parts); err != nil {
		return nil, compatibility.BadRequest(surface + " message content is invalid")
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
			if text == "" {
				return nil, compatibility.BadRequest(surface + " text parts must not be empty")
			}
			decoded = append(decoded, compatibility.NewTextItem(author, text))
		default:
			return nil, compatibility.BadRequest(surface + " message content contains an unsupported part type")
		}
	}
	return decoded, nil
}

func authorForRole(role string) compatibility.ItemAuthor {
	switch strings.TrimSpace(role) {
	case "assistant":
		return compatibility.ItemAuthorAssistant
	case "tool":
		return compatibility.ItemAuthorTool
	default:
		return compatibility.ItemAuthorUser
	}
}

func joinItemText(items []compatibility.CanonicalItem) string {
	var builder strings.Builder
	for _, item := range items {
		if item.Kind != compatibility.ItemKindText {
			continue
		}
		builder.WriteString(item.Text)
	}
	return builder.String()
}

type chatCompletionClientStreamEncoder struct {
	resultID string
	model    string
	started  bool
	toolByID map[string]int
}

// event-to-frame fanout over text, tool calls, and terminal envelopes.
func (s *chatCompletionClientStreamEncoder) Encode(event compatibility.OutputEvent) ([][]byte, error) {
	if s.toolByID == nil {
		s.toolByID = map[string]int{}
	}
	switch event.Kind {
	case compatibility.OutputEventStarted:
		s.resultID = fallbackID(event.ResultID, "chatcmpl_swobu")
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
		return [][]byte{sseData(raw)}, nil
	case compatibility.OutputEventItemStarted:
		if event.ItemKind == compatibility.ItemKindToolUse {
			index := len(s.toolByID)
			s.toolByID[event.ItemID] = index
			raw, _ := json.Marshal(chatCompletionsResponseDTO{
				ID:     fallbackID(s.resultID, "chatcmpl_swobu"),
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
			return [][]byte{sseData(raw)}, nil
		}
		return nil, nil
	case compatibility.OutputEventTextDelta:
		if !s.started {
			frames, _ := s.Encode(compatibility.OutputEvent{Kind: compatibility.OutputEventStarted, ResultID: s.resultID, Model: s.model})
			more, err := s.Encode(event)
			return append(frames, more...), err
		}
		raw, _ := json.Marshal(chatCompletionsResponseDTO{
			ID:     fallbackID(s.resultID, "chatcmpl_swobu"),
			Object: "chat.completion.chunk",
			Model:  s.model,
			Choices: []chatCompletionsChoiceDTO{{
				Index: 0,
				Delta: &chatCompletionsDeltaDTO{Content: event.TextDelta},
			}},
		})
		return [][]byte{sseData(raw)}, nil
	case compatibility.OutputEventToolUseArgumentsDelta:
		index, ok := s.toolByID[event.ItemID]
		if !ok {
			startFrames, err := s.Encode(compatibility.OutputEvent{
				Kind:      compatibility.OutputEventItemStarted,
				ItemKind:  compatibility.ItemKindToolUse,
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
			ID:     fallbackID(s.resultID, "chatcmpl_swobu"),
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
		return [][]byte{sseData(raw)}, nil
	case compatibility.OutputEventItemCompleted:
		return nil, nil
	case compatibility.OutputEventCompleted:
		raw, _ := json.Marshal(chatCompletionsResponseDTO{
			ID:     fallbackID(s.resultID, "chatcmpl_swobu"),
			Object: "chat.completion.chunk",
			Model:  s.model,
			Choices: []chatCompletionsChoiceDTO{{
				Index:        0,
				Delta:        &chatCompletionsDeltaDTO{},
				FinishReason: defaultFinishReason(event.FinishReason, "stop"),
			}},
			Usage: chatUsageFromCanonical(event.Usage),
		})
		return [][]byte{
			sseData(raw),
			[]byte("data: [DONE]\n\n"),
		}, nil
	default:
		return nil, compatibility.UnsupportedOperation("chat completions streaming event is not implemented")
	}
}

func (s *chatCompletionClientStreamEncoder) Finish() ([][]byte, error) { return nil, nil }

func chatMessageFromOutput(output compatibility.CanonicalOutput) chatCompletionsResponseMessageDTO {
	text := outputText(output.Items())
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

func chatToolCalls(items []compatibility.OutputItem) []chatCompletionsResponseToolCallDTO {
	out := make([]chatCompletionsResponseToolCallDTO, 0)
	for _, item := range items {
		if item.Kind != compatibility.ItemKindToolUse {
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

func generatedToolUseID(msgIdx int, partIdx int) string {
	return "toolu_swobu_" + strconv.Itoa(msgIdx) + "_" + strconv.Itoa(partIdx)
}
