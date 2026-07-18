package chatcompletions

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	openaiwire "github.com/swobuforge/swobu/internal/wire/openai"
)

func decodeResponseOutputItems(content json.RawMessage, toolCalls []toolCallBody) ([]canonical.OutputItem, error) {
	items, err := decodeOpenAIContentItems(content)
	if err != nil {
		return nil, canonical.InternalError("chat completions response content is unsupported")
	}
	out := make([]canonical.OutputItem, 0, len(items)+len(toolCalls))
	for idx, item := range items {
		if item.Kind != canonical.ItemKindText {
			continue
		}
		out = append(out, canonical.NewTextOutputItem("text_"+strconv.Itoa(idx), item.Text))
	}
	for _, call := range toolCalls {
		itemID := strings.TrimSpace(call.ID) // swobu:io-string source=boundary
		if itemID == "" {
			itemID = "tool_0"
		}
		toolUseID := itemID
		normalizedType := strings.ToLower(strings.TrimSpace(call.Type)) // swobu:io-string source=provider-wire
		switch normalizedType {
		case "function":
			if call.Function == nil {
				return nil, canonical.InternalError("chat completions response function tool call is incomplete")
			}
			functionName := strings.TrimSpace(call.Function.Name) // swobu:io-string source=boundary
			if functionName == "" {
				return nil, canonical.InternalError("chat completions response function tool call is missing a name")
			}
			out = append(out, canonical.NewToolUseOutputItem(
				itemID,
				toolUseID,
				functionName,
				canonical.NewToolArgumentsObject(call.Function.Arguments),
			))
		case "custom":
			if call.Custom == nil {
				return nil, canonical.InternalError("chat completions response custom tool call is incomplete")
			}
			customName := strings.TrimSpace(call.Custom.Name) // swobu:io-string source=boundary
			if customName == "" {
				return nil, canonical.InternalError("chat completions response custom tool call is missing a name")
			}
			out = append(out, canonical.NewCustomToolUseOutputItem(
				itemID,
				toolUseID,
				customName,
				canonical.NewToolArgumentsObject(call.Custom.Input),
			))
		case "":
			if call.Function != nil && call.Custom == nil {
				functionName := strings.TrimSpace(call.Function.Name) // swobu:io-string source=boundary
				if functionName == "" {
					return nil, canonical.InternalError("chat completions response function tool call is missing a name")
				}
				out = append(out, canonical.NewToolUseOutputItem(
					itemID,
					toolUseID,
					functionName,
					canonical.NewToolArgumentsObject(call.Function.Arguments),
				))
				continue
			}
			if call.Custom != nil && call.Function == nil {
				customName := strings.TrimSpace(call.Custom.Name) // swobu:io-string source=boundary
				if customName == "" {
					return nil, canonical.InternalError("chat completions response custom tool call is missing a name")
				}
				out = append(out, canonical.NewCustomToolUseOutputItem(
					itemID,
					toolUseID,
					customName,
					canonical.NewToolArgumentsObject(call.Custom.Input),
				))
				continue
			}
			return nil, canonical.InternalError("chat completions response tool call type is unsupported")
		default:
			return nil, canonical.InternalError("chat completions response tool call type is unsupported")
		}
	}
	return out, nil
}

func decodeOpenAIContentItems(raw json.RawMessage) ([]canonical.CanonicalItem, error) {
	parts, err := openaiwire.DecodeContentParts(raw, "chat completions response content is invalid")
	if err != nil {
		return nil, err
	}
	decoded := make([]canonical.CanonicalItem, 0, len(parts))
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
				decoded = append(decoded, canonical.NewTextItem(canonical.ItemAuthorAssistant, text))
			}
		default:
			return canonical.UnsupportedOperation("chat completions response content contains an unsupported part type")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return decoded, nil
}
