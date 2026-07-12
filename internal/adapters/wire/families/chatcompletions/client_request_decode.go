package chatcompletions

import (
	"encoding/json"
	"strings"

	sse "github.com/swobuforge/swobu/internal/adapters/wire/framing/sse"
	core "github.com/swobuforge/swobu/internal/adapters/wire/primitives"
	openaicompat "github.com/swobuforge/swobu/internal/adapters/wire/shared/openaicompat"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func (ClientRequestDecoder) DecodeClientRequest(doc carrier.WireDocument) (canonical.CanonicalRequest, delivery.Delivery, error) {
	raw := doc.RawBytes()
	var dto chatCompletionsRequestDTO
	if err := sse.DecodePermissiveJSON(raw, &dto, "chat completions request", nil); err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	if len(dto.Messages) == 0 {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), canonical.BadRequest("chat completions request is missing required fields")
	}
	tools, err := decodeChatCompletionsTools(dto.Tools)
	if err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	toolPolicy, err := decodeChatCompletionsToolChoice(dto.ToolChoice, tools)
	if err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	toolCallBatch, err := decodeChatCompletionsToolCallBatch(dto.ParallelToolCalls)
	if err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	streamRequested, err := core.DecodeRequestStreamFlag(raw, "chat completions")
	if err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	items := make([]canonical.CanonicalItem, 0, len(dto.Messages))
	for idx, msg := range dto.Messages {
		decoded, err := decodeChatCompletionsItems(msg.Role, msg.Content, msg.ToolCalls, msg.ToolCallID, idx)
		if err != nil {
			return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
		}
		items = append(items, decoded...)
	}
	controls, err := decodeChatCompletionsGenerationControls(dto)
	if err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	outputFormat, err := decodeChatCompletionsOutputFormat(dto.ResponseFormat)
	if err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	resolvedDelivery := delivery.BufferedDelivery()
	if streamRequested {
		resolvedDelivery = delivery.StreamingDelivery(delivery.FramingNone)
	}
	return canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:         strings.TrimSpace(dto.Model), // swobu:io-string source=boundary
		Items:         items,
		Tools:         tools,
		ToolPolicy:    toolPolicy,
		ToolCallBatch: toolCallBatch,
		Controls:      controls,
		OutputFormat:  outputFormat,
	}), resolvedDelivery, nil
}

// swobu:lint ignore string-switch because=protocol boundary decodes wire tool-call kinds.
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
		id := strings.TrimSpace(call.ID) // swobu:io-string source=boundary
		if id == "" {
			id = openaicompat.GeneratedToolUseID(msgIdx, idx)
		}
		switch strings.ToLower(strings.TrimSpace(call.Type)) {
		case "", "function":
			if call.Function == nil {
				return nil, canonical.BadRequest("chat completions tool calls require a function body")
			}
			if strings.TrimSpace(call.Function.Name) == "" { // swobu:io-string source=boundary
				return nil, canonical.BadRequest("chat completions tool calls require a function name")
			}
			input, err := sse.DecodeJSONObject(call.Function.Arguments, "chat completions tool call arguments are invalid")
			if err != nil {
				return nil, err
			}
			args, err := json.Marshal(input)
			if err != nil {
				return nil, canonical.BadRequest("chat completions tool call arguments are invalid")
			}
			items = append(items, canonical.NewToolUseItem(author, "", id, strings.TrimSpace(call.Function.Name), canonical.NewToolArgumentsObject(string(args)))) // swobu:io-string source=boundary
		case "custom":
			if call.Custom == nil {
				return nil, canonical.BadRequest("chat completions custom tool calls require a custom body")
			}
			if strings.TrimSpace(call.Custom.Name) == "" { // swobu:io-string source=boundary
				return nil, canonical.BadRequest("chat completions custom tool calls require a custom name")
			}
			items = append(items, canonical.NewCustomToolUseItem(author, "", id, strings.TrimSpace(call.Custom.Name), canonical.NewToolArgumentsObject(call.Custom.Input))) // swobu:io-string source=boundary
		default:
			return nil, canonical.BadRequest("chat completions request contains an unsupported tool call type")
		}
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
