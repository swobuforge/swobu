// translation in one place so request and stream semantics stay recoverable.
package chatcompletions

import (
	"encoding/json"
	"strings"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	sse "github.com/swobuforge/swobu/internal/wire/framing/sse"
)

type chatCompletionsEnvelopeStreamEncoder struct {
	resultID string
	model    string
	started  bool
	toolByID map[string]int
	toolKind map[string]string
	adapter  *sse.EnvelopeEventAdapter

	pendingWebSearchCalls map[string]uint32
	sawReasoning          bool
	sawVisibleOutput      bool
	changes               []compat.Change
	includeUsageFrame     bool
}

func (s *chatCompletionsEnvelopeStreamEncoder) Changes() []compat.Change {
	return compat.CloneChanges(s.changes)
}

func (s *chatCompletionsEnvelopeStreamEncoder) EncodeEnvelopeEvent(event canonical.Event) ([][]byte, error) {
	streamEvents, err := s.adapter.Translate(event)
	if err != nil {
		return nil, err
	}
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
		s.toolKind = map[string]string{}
	}
	switch event.Kind {
	case sse.StreamEventStarted:
		s.resultID = sse.FallbackID(event.ResultID, "chatcmpl_swobu")
		s.model = event.Model
		s.started = true
		return s.encodeChunk(chatCompletionsDeltaDTO{Role: "assistant"}, nil, nil)
	case sse.StreamEventItemStarted:
		if event.ItemKind == canonical.ItemKindReasoning {
			s.sawReasoning = true
			return nil, nil
		}
		if event.ItemKind == canonical.ItemKindToolCall {
			kind := strings.ToLower(strings.TrimSpace(event.ToolType))
			if kind == canonical.ToolTypeWebSearch {
				s.pendingWebSearchCalls[event.ToolUseID] = event.ItemOrdinal
				return nil, nil
			}
			index := len(s.toolByID)
			s.toolByID[event.ItemID] = index
			s.toolKind[event.ItemID] = kind
			call := chatCompletionsDeltaToolCallDTO{Index: index, ID: event.ToolUseID, Type: kind}
			switch kind {
			case canonical.ToolTypeFunction:
				call.Function = &chatCompletionsDeltaFunctionDTO{Name: event.Name}
			case canonical.ToolTypeCustom:
				call.Custom = &chatCompletionsResponseCustomDTO{Name: event.Name}
			default:
				return nil, canonical.InternalError("Chat Completions stream received an unsupported canonical tool-call kind")
			}
			s.sawVisibleOutput = true
			return s.encodeChunk(chatCompletionsDeltaDTO{
				ToolCalls: []chatCompletionsDeltaToolCallDTO{call},
			}, nil, nil)
		}
		return nil, nil
	case sse.StreamEventContentStarted:
		// Chat Completions has no content-part index; accepting the lifecycle
		// marker does not erase its coordinates from protocols that do.
		return nil, nil
	case sse.StreamEventTextDelta:
		if !s.started {
			frames, _ := s.Encode(sse.StreamEvent{Kind: sse.StreamEventStarted, ResultID: s.resultID, Model: s.model})
			more, err := s.Encode(event)
			return append(frames, more...), err
		}
		if event.TextDelta != "" {
			s.sawVisibleOutput = true
		}
		return s.encodeChunk(chatCompletionsDeltaDTO{Content: event.TextDelta}, nil, nil)
	case sse.StreamEventToolUseArgumentsDelta:
		index, ok := s.toolByID[event.ItemID]
		if !ok {
			startFrames, err := s.Encode(sse.StreamEvent{
				Kind:      sse.StreamEventItemStarted,
				ItemKind:  canonical.ItemKindToolCall,
				ResultID:  s.resultID,
				Model:     s.model,
				ItemID:    event.ItemID,
				ToolUseID: event.ToolUseID,
				Name:      event.Name,
				ToolType:  event.ToolType,
			})
			if err != nil {
				return nil, err
			}
			frame, err := s.Encode(event)
			return append(startFrames, frame...), err
		}
		kind := s.toolKind[event.ItemID]
		call := chatCompletionsDeltaToolCallDTO{Index: index}
		switch kind {
		case canonical.ToolTypeFunction:
			call.Function = &chatCompletionsDeltaFunctionDTO{Arguments: event.ArgumentsDelta}
		case canonical.ToolTypeCustom:
			call.Custom = &chatCompletionsResponseCustomDTO{Input: event.ArgumentsDelta}
		default:
			return nil, canonical.InternalError("Chat Completions stream received an unknown canonical tool-input kind")
		}
		return s.encodeChunk(chatCompletionsDeltaDTO{
			ToolCalls: []chatCompletionsDeltaToolCallDTO{call},
		}, nil, nil)
	case sse.StreamEventItemCompleted:
		if event.CompletedItem != nil {
			if event.CompletedItem.Kind() == canonical.ItemKindReasoning {
				s.sawReasoning = true
				return nil, nil
			}
			if call, ok := event.CompletedItem.ToolCall(); ok && call.Tool().Kind() == canonical.ToolKindWebSearch {
				return nil, nil
			}
			if result, ok := event.CompletedItem.ToolResult(); ok {
				if _, webSearch := result.WebSearch(); webSearch {
					callID := result.CallID().String()
					callOrdinal, pending := s.pendingWebSearchCalls[callID]
					if !pending {
						return nil, canonical.NewBackendError("", 0, "backend returned an orphan web-search result to a Chat Completions client", "")
					}
					delete(s.pendingWebSearchCalls, callID)
					s.changes = append(s.changes, compat.Change{
						Capability: canonical.ResponseItemsKind,
						Kind:       compat.Omission,
						Occurrence: canonical.ResponseItemOccurrence(callOrdinal),
					})
					return nil, nil
				}
			}
			citationDecisions := chatCompletionsCitationDropDecisions(event.ItemOrdinal, *event.CompletedItem)
			s.changes = append(s.changes, citationDecisions...)
		}
		return nil, nil
	case sse.StreamEventCompleted:
		if len(s.pendingWebSearchCalls) > 0 {
			return nil, canonical.NewBackendError("", 0, "backend returned an unresolved web-search lifecycle to a Chat Completions client", "")
		}
		clientFinishReason, err := chatClientFinishReason(event.Completion, len(s.toolByID) > 0)
		if err != nil {
			return nil, err
		}
		projectionDecisions, projectionErr := finalizeChatClientProjection(s.sawReasoning, s.sawVisibleOutput, clientFinishReason)
		s.changes = append(s.changes, projectionDecisions...)
		if projectionErr != nil {
			return nil, projectionErr
		}
		frames, err := s.encodeChunk(chatCompletionsDeltaDTO{}, &clientFinishReason, nil)
		if err != nil {
			return nil, err
		}
		if s.includeUsageFrame {
			if usage := chatUsageFromCanonical(event.Usage); usage != nil {
				usageFrame, usageErr := s.encodeUsageChunk(usage)
				if usageErr != nil {
					return nil, usageErr
				}
				frames = append(frames, usageFrame)
			}
		}
		return append(frames, []byte("data: [DONE]\n\n")), nil
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
		return nil, canonical.InternalError("Chat Completions stream received an unknown canonical event kind")
	}
}

func (s *chatCompletionsEnvelopeStreamEncoder) encodeChunk(
	delta chatCompletionsDeltaDTO,
	finishReason *string,
	usage *chatCompletionsUsageDTO,
) ([][]byte, error) {
	raw, err := json.Marshal(chatCompletionsStreamResponseDTO{
		ID:     sse.FallbackID(s.resultID, "chatcmpl_swobu"),
		Object: "chat.completion.chunk",
		Model:  s.model,
		Choices: []chatCompletionsStreamChoiceDTO{{
			Index:        0,
			Delta:        delta,
			FinishReason: finishReason,
		}},
		Usage: usage,
	})
	if err != nil {
		return nil, canonical.InternalError("Chat Completions stream chunk could not be encoded")
	}
	return [][]byte{sse.SSEData(raw)}, nil
}

func (s *chatCompletionsEnvelopeStreamEncoder) encodeUsageChunk(usage *chatCompletionsUsageDTO) ([]byte, error) {
	raw, err := json.Marshal(chatCompletionsStreamResponseDTO{
		ID: sse.FallbackID(s.resultID, "chatcmpl_swobu"), Object: "chat.completion.chunk", Model: s.model,
		Choices: []chatCompletionsStreamChoiceDTO{}, Usage: usage,
	})
	if err != nil {
		return nil, canonical.InternalError("Chat Completions usage chunk could not be encoded")
	}
	return sse.SSEData(raw), nil
}

func (s *chatCompletionsEnvelopeStreamEncoder) Finish() ([][]byte, error) { return nil, nil }

func chatMessageFromItems(items []canonical.CanonicalItem) (chatCompletionsResponseMessageDTO, error) {
	message := chatCompletionsResponseMessageDTO{
		Role: "assistant",
	}
	var text strings.Builder
	toolCalls := make([]chatCompletionsResponseToolCallDTO, 0)
	for _, item := range items {
		switch item.Kind() {
		case canonical.ItemKindMessage:
			messageItem, _ := item.Message()
			if messageItem.Role() != canonical.MessageRoleAssistant {
				return chatCompletionsResponseMessageDTO{}, canonical.InternalError("canonical response contains a non-assistant Chat Completions output message")
			}
			content, err := chatClientTextContent(messageItem.Content(), "chat completions responses")
			if err != nil {
				return chatCompletionsResponseMessageDTO{}, err
			}
			text.WriteString(content)
		case canonical.ItemKindToolCall:
			wire, err := chatToolCallFromOutputItem(item)
			if err != nil {
				return chatCompletionsResponseMessageDTO{}, err
			}
			toolCalls = append(toolCalls, wire)
		case canonical.ItemKindReasoning:
			// Standard Chat Completions has no reasoning item representation.
			// The canonical item and opaque thinking remain in session truth;
			// the client projection never exposes backend dialect fields.
			continue
		default:
			return chatCompletionsResponseMessageDTO{}, canonical.InternalError("Chat Completions output received an unsupported canonical item kind")
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
func chatToolCallFromOutputItem(item canonical.CanonicalItem) (chatCompletionsResponseToolCallDTO, error) {
	call, ok := item.ToolCall()
	if !ok {
		return chatCompletionsResponseToolCallDTO{}, canonical.InternalError("chat completions tool-call item payload is invalid")
	}
	tool := call.Tool()
	name := tool.Name()
	switch tool.Kind() {
	case canonical.ToolKindFunction:
		object, ok := call.Input().Object()
		if !ok {
			return chatCompletionsResponseToolCallDTO{}, canonical.BadRequest("chat completions response function call requires object input")
		}
		return chatCompletionsResponseToolCallDTO{
			ID:   call.CallID().String(),
			Type: "function",
			Function: &chatCompletionsResponseFunctionDTO{
				Name:      name,
				Arguments: object.String(),
			},
		}, nil
	case canonical.ToolKindCustom:
		text, ok := call.Input().Text()
		if !ok {
			return chatCompletionsResponseToolCallDTO{}, canonical.BadRequest("chat completions response custom call requires text input")
		}
		return chatCompletionsResponseToolCallDTO{
			ID:   call.CallID().String(),
			Type: "custom",
			Custom: &chatCompletionsResponseCustomDTO{
				Name:  name,
				Input: text,
			},
		}, nil
	default:
		return chatCompletionsResponseToolCallDTO{}, canonical.InternalError("Chat Completions output received an unknown canonical tool-call kind")
	}
}

func chatUsageFromCanonical(usage canonical.TokenUsage) *chatCompletionsUsageDTO {
	input, hasInput := usage.InputTokens()
	output, hasOutput := usage.OutputTokens()
	reasoning, hasReasoning := usage.ReasoningTokens()
	cacheRead, hasCacheRead := usage.CacheReadTokens()
	cacheWrite, hasCacheWrite := usage.CacheWriteTokens()
	if !hasInput || !hasOutput {
		return nil
	}
	total := input + output
	dto := &chatCompletionsUsageDTO{
		PromptTokens:     input,
		CompletionTokens: output,
		TotalTokens:      total,
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
