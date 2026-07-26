package chatcompletions

import (
	"encoding/json"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	openaiwire "github.com/swobuforge/swobu/internal/wire/openai"
)

func decodeResponseOutputItems(request canonical.CanonicalRequest, content json.RawMessage, toolCalls []toolCallBody) ([]canonical.CanonicalItem, error) {
	message, hasMessage, err := decodeOpenAIContentMessage(content)
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
	for _, call := range toolCalls {
		callID, err := canonical.NewToolCallID(call.ID)
		if err != nil {
			return nil, canonical.InternalError("chat completions response tool call is missing an id")
		}
		normalizedType := strings.ToLower(strings.TrimSpace(call.Type)) // swobu:io-string source=provider-wire
		if normalizedType == "" && call.Function != nil && call.Custom == nil {
			normalizedType = canonical.ToolTypeFunction
		}
		if normalizedType == "" && call.Custom != nil && call.Function == nil {
			normalizedType = canonical.ToolTypeCustom
		}
		switch normalizedType {
		case "function":
			if call.Function == nil {
				return nil, canonical.InternalError("chat completions response function tool call is incomplete")
			}
			functionName := strings.TrimSpace(call.Function.Name) // swobu:io-string source=boundary
			if functionName == "" {
				return nil, canonical.InternalError("chat completions response function tool call is missing a name")
			}
			resolved, _, err := canonical.ResolveToolDeclarationByName(tools, functionName, canonical.ToolTypeFunction)
			if err != nil {
				return nil, canonical.InternalError("chat completions response references an unknown or ambiguous function tool")
			}
			object, err := canonical.ParseJSONObject([]byte(call.Function.Arguments))
			if err != nil {
				return nil, canonical.InternalError("chat completions response function arguments are invalid")
			}
			item, err := canonical.NewToolCallItem(callID, resolved.Key(), canonical.NewJSONObjectToolInput(object))
			if err != nil {
				return nil, canonical.InternalError("chat completions response function call is invalid")
			}
			out = append(out, item)
		case "custom":
			if call.Custom == nil {
				return nil, canonical.InternalError("chat completions response custom tool call is incomplete")
			}
			customName := strings.TrimSpace(call.Custom.Name) // swobu:io-string source=boundary
			if customName == "" {
				return nil, canonical.InternalError("chat completions response custom tool call is missing a name")
			}
			resolved, _, err := canonical.ResolveToolDeclarationByName(tools, customName, canonical.ToolTypeCustom)
			if err != nil {
				return nil, canonical.InternalError("chat completions response references an unknown or ambiguous custom tool")
			}
			item, err := canonical.NewToolCallItem(callID, resolved.Key(), canonical.NewTextToolInput(call.Custom.Input))
			if err != nil {
				return nil, canonical.InternalError("chat completions response custom call is invalid")
			}
			out = append(out, item)
		default:
			return nil, canonical.InternalError("chat completions response tool call type is unsupported")
		}
	}
	return out, nil
}

func decodeOpenAIContentMessage(raw json.RawMessage) (canonical.CanonicalItem, bool, error) {
	parts, err := openaiwire.DecodeContentParts(raw, "chat completions response content is invalid")
	if err != nil {
		return canonical.CanonicalItem{}, false, err
	}
	content := make([]canonical.MessagePart, 0, len(parts))
	err = openaiwire.WalkContentParts(parts, func(_ int, part openaiwire.ContentPartItem) error {
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
			return canonical.NotImplemented("Swobu has no canonical projection for this Chat Completions response content part type")
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
