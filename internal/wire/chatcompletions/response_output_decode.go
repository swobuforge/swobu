package chatcompletions

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	openaiwire "github.com/swobuforge/swobu/internal/wire/openai"
)

// admitChatToolCallUnion classifies one buffered occurrence or stream
// fragment. Streaming may retain an unresolved ID-only fragment, but a
// complete occurrence must select exactly one known body unless an explicit
// unfamiliar discriminator erases the whole occurrence.
func admitChatToolCallUnion(rawType string, hasFunction, hasCustom, complete bool) (kind string, erased bool, err error) {
	kind = strings.ToLower(strings.TrimSpace(rawType)) // swobu:io-string source=provider-wire
	switch kind {
	case canonical.ToolTypeFunction:
		if hasCustom || complete && !hasFunction {
			return "", false, canonical.NewBackendError("", 0, "chat completions function tool call has a contradictory body", "")
		}
		return kind, false, nil
	case canonical.ToolTypeCustom:
		if hasFunction || complete && !hasCustom {
			return "", false, canonical.NewBackendError("", 0, "chat completions custom tool call has a contradictory body", "")
		}
		return kind, false, nil
	case "":
		switch {
		case hasFunction && hasCustom:
			return "", false, canonical.NewBackendError("", 0, "chat completions tool call has mutually exclusive bodies", "")
		case hasFunction:
			return canonical.ToolTypeFunction, false, nil
		case hasCustom:
			return canonical.ToolTypeCustom, false, nil
		case complete:
			return "", false, canonical.NewBackendError("", 0, "chat completions tool call has no inferable variant", "")
		default:
			return "", false, nil
		}
	default:
		return kind, true, nil
	}
}

func decodeResponseOutputItems(request canonical.CanonicalRequest, content json.RawMessage, toolCalls []toolCallBody, sink compat.Sink, exchangeID string) ([]canonical.CanonicalItem, error) {
	message, hasMessage, err := decodeOpenAIContentMessage(content, sink, exchangeID)
	if err != nil {
		return nil, canonical.InternalError("chat completions response content is unsupported")
	}
	environment, err := canonical.EffectiveTools(request)
	if err != nil {
		return nil, canonical.InternalError("chat completions tool environment is ambiguous")
	}
	tools := environment.Declarations()
	out := make([]canonical.CanonicalItem, 0, 1+len(toolCalls))
	if hasMessage {
		out = append(out, message)
	}
	for index, call := range toolCalls {
		normalizedType, erased, err := admitChatToolCallUnion(call.Type, call.Function != nil, call.Custom != nil, true)
		if err != nil {
			return nil, err
		}
		if erased {
			if err := emitChatCompletionsCompatibilityDecision(sink, exchangeID, compat.ResponseItemsKind, compat.Drop, compat.Subject(fmt.Sprintf("wire:/choices/0/message/tool_calls/%d/type", index))); err != nil {
				return nil, err
			}
			continue
		}
		callID, err := canonical.NewToolCallID(call.ID)
		if err != nil {
			return nil, canonical.NewBackendError("", 0, "chat completions response tool call is missing an id", "")
		}
		switch normalizedType {
		case "function":
			if call.Function == nil {
				return nil, canonical.NewBackendError("", 0, "chat completions response function tool call is incomplete", "")
			}
			functionName := strings.TrimSpace(call.Function.Name) // swobu:io-string source=boundary
			if functionName == "" {
				return nil, canonical.NewBackendError("", 0, "chat completions response function tool call is missing a name", "")
			}
			resolved, _, err := canonical.ResolveToolDeclarationByName(tools, functionName, canonical.ToolTypeFunction)
			if err != nil {
				return nil, canonical.NewBackendError("", 0, "chat completions response references an unknown or ambiguous function tool", "")
			}
			object, err := canonical.ParseJSONObject([]byte(call.Function.Arguments))
			if err != nil {
				return nil, canonical.NewBackendError("", 0, "chat completions response function arguments are invalid", "")
			}
			item, err := canonical.NewToolCallItem(callID, resolved.Key(), canonical.NewJSONObjectToolInput(object))
			if err != nil {
				return nil, canonical.NewBackendError("", 0, "chat completions response function call is invalid", "")
			}
			out = append(out, item)
		case "custom":
			if call.Custom == nil {
				return nil, canonical.NewBackendError("", 0, "chat completions response custom tool call is incomplete", "")
			}
			customName := strings.TrimSpace(call.Custom.Name) // swobu:io-string source=boundary
			if customName == "" {
				return nil, canonical.NewBackendError("", 0, "chat completions response custom tool call is missing a name", "")
			}
			resolved, _, err := canonical.ResolveToolDeclarationByName(tools, customName, canonical.ToolTypeCustom)
			if err != nil {
				return nil, canonical.NewBackendError("", 0, "chat completions response references an unknown or ambiguous custom tool", "")
			}
			item, err := canonical.NewToolCallItem(callID, resolved.Key(), canonical.NewTextToolInput(call.Custom.Input))
			if err != nil {
				return nil, canonical.NewBackendError("", 0, "chat completions response custom call is invalid", "")
			}
			out = append(out, item)
		}
	}
	return out, nil
}

func decodeOpenAIContentMessage(raw json.RawMessage, sink compat.Sink, exchangeID string) (canonical.CanonicalItem, bool, error) {
	parts, err := openaiwire.DecodeContentParts(raw, "chat completions response content is invalid")
	if err != nil {
		return canonical.CanonicalItem{}, false, err
	}
	content := make([]canonical.MessagePart, 0, len(parts))
	err = openaiwire.WalkContentParts(parts, func(index int, part openaiwire.ContentPartItem) error {
		partType := strings.TrimSpace(part.Type) // swobu:io-string source=boundary // swobu:io-string source=provider-wire
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
				content = append(content, canonical.NewTextMessagePart(text))
			}
		default:
			return emitChatCompletionsCompatibilityDecision(sink, exchangeID, compat.ResponseItemsKind, compat.Drop, compat.Subject(fmt.Sprintf("wire:/choices/0/message/content/%d/type", index)))
		}
		return nil
	})
	if err != nil {
		return canonical.CanonicalItem{}, false, err
	}
	if len(content) == 0 {
		return canonical.CanonicalItem{}, false, nil
	}
	message, err := canonical.NewMessageItem(canonical.MessageRoleAssistant, content)
	if err != nil {
		return canonical.CanonicalItem{}, false, err
	}
	return message, true, nil
}
