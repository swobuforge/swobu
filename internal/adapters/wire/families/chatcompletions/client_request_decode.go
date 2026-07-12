package chatcompletions

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	sse "github.com/swobuforge/swobu/internal/adapters/wire/framing/sse"
	core "github.com/swobuforge/swobu/internal/adapters/wire/primitives"
	shared "github.com/swobuforge/swobu/internal/adapters/wire/shared"
	openaicompat "github.com/swobuforge/swobu/internal/adapters/wire/shared/openaicompat"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/effect"
	"github.com/swobuforge/swobu/internal/exchange"
)

func (ClientRequestDecoder) DecodeClientRequest(doc carrier.WireDocument) (exchange.Result[exchange.ClientRequestDecode], error) {
	return shared.WithAccumulatedEffects(func(sink effect.Sink) (exchange.ClientRequestDecode, error) {
		request, delivery, err := (ClientRequestDecoder{}).decodeClientRequestWithEffects(doc, sink, "")
		return exchange.ClientRequestDecode{
			Request:  request,
			Delivery: delivery,
		}, err
	})
}

func (ClientRequestDecoder) decodeClientRequestWithEffects(doc carrier.WireDocument, sink effect.Sink, exchangeID string) (canonical.CanonicalRequest, delivery.Delivery, error) {
	raw := doc.RawBytes()
	var dto chatCompletionsRequestDTO
	if err := sse.DecodePermissiveJSON(raw, &dto, "chat completions request", nil); err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	if len(dto.Messages) == 0 {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), canonical.BadRequest("chat completions request is missing required fields")
	}
	tools, err := decodeChatCompletionsTools(dto.Tools, sink, exchangeID)
	if err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	toolPolicy, err := decodeChatCompletionsToolChoice(dto.ToolChoice, tools, sink, exchangeID)
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
		decoded, err := decodeChatCompletionsItems(sink, exchangeID, msg.Role, msg.Content, msg.ToolCalls, msg.ToolCallID, idx)
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
	sink effect.Sink,
	exchangeID string,
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
			if err := emitChatCompletionsCompatibilityDecision(sink, exchangeID, compat.ToolCallID, compat.Approx, chatCompletionsToolSubject(msgIdx, 0, "tool_call_id")); err != nil {
				return nil, err
			}
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
			if err := emitChatCompletionsCompatibilityDecision(sink, exchangeID, compat.ToolCallID, compat.Approx, chatCompletionsToolSubject(msgIdx, idx, "id")); err != nil {
				return nil, err
			}
			id = openaicompat.GeneratedToolUseID(msgIdx, idx)
		}
		switch strings.ToLower(strings.TrimSpace(call.Type)) { // swobu:io-string source=domain
		case "", "function":
			if call.Function == nil {
				if err := emitChatCompletionsCompatibilityDecision(sink, exchangeID, compat.ToolCallArguments, compat.Reject, chatCompletionsToolSubject(msgIdx, idx, "function/arguments")); err != nil {
					return nil, err
				}
				return nil, canonical.BadRequest("chat completions tool calls require a function body")
			}
			if strings.TrimSpace(call.Function.Name) == "" { // swobu:io-string source=boundary
				return nil, canonical.BadRequest("chat completions tool calls require a function name")
			}
			input, err := decodeChatCompletionsFunctionArguments(call.Function.Arguments)
			if err != nil {
				if err := emitChatCompletionsCompatibilityDecision(sink, exchangeID, compat.ToolCallArguments, compat.Reject, chatCompletionsToolSubject(msgIdx, idx, "function/arguments")); err != nil {
					return nil, err
				}
				return nil, err
			}
			args, err := json.Marshal(input)
			if err != nil {
				if err := emitChatCompletionsCompatibilityDecision(sink, exchangeID, compat.ToolCallArguments, compat.Reject, chatCompletionsToolSubject(msgIdx, idx, "function/arguments")); err != nil {
					return nil, err
				}
				return nil, canonical.BadRequest("chat completions tool call arguments are invalid")
			}
			items = append(items, canonical.NewToolUseItem(author, "", id, strings.TrimSpace(call.Function.Name), canonical.NewToolArgumentsObject(string(args)))) // swobu:io-string source=boundary
		case "custom":
			if call.Custom == nil {
				if err := emitChatCompletionsCompatibilityDecision(sink, exchangeID, compat.ToolCallArguments, compat.Reject, chatCompletionsToolSubject(msgIdx, idx, "custom/input")); err != nil {
					return nil, err
				}
				return nil, canonical.BadRequest("chat completions custom tool calls require a custom body")
			}
			if strings.TrimSpace(call.Custom.Name) == "" { // swobu:io-string source=boundary
				return nil, canonical.BadRequest("chat completions custom tool calls require a custom name")
			}
			items = append(items, canonical.NewCustomToolUseItem(author, "", id, strings.TrimSpace(call.Custom.Name), canonical.NewToolArgumentsObject(call.Custom.Input))) // swobu:io-string source=boundary
		default:
			if err := emitChatCompletionsCompatibilityDecision(sink, exchangeID, compat.ToolCallKind, compat.Reject, chatCompletionsToolSubject(msgIdx, idx, "type")); err != nil {
				return nil, err
			}
			return nil, canonical.BadRequest("chat completions request contains an unsupported tool call type")
		}
	}
	return items, nil
}

// OpenCode and other OpenAI-family chat-completions bridges may stringify
// function call arguments when they reconstruct a prior tool call. Accept both
// the wire object and the stringified object, then normalize to canonical JSON.
func decodeChatCompletionsFunctionArguments(raw json.RawMessage) (map[string]any, error) {
	input, err := sse.DecodeJSONObject(raw, "chat completions tool call arguments are invalid")
	if err == nil {
		return input, nil
	}
	var stringified string
	if err := json.Unmarshal(raw, &stringified); err != nil {
		return nil, canonical.BadRequest("chat completions tool call arguments are invalid")
	}
	return sse.DecodeJSONObject(json.RawMessage(strings.TrimSpace(stringified)), "chat completions tool call arguments are invalid")
}

func emitChatCompletionsCompatibilityDecision(sink effect.Sink, exchangeID string, feature compat.Feature, outcome compat.Outcome, subject compat.Subject) error {
	if sink == nil {
		return nil
	}
	if subject == "" {
		return nil
	}
	if err := sink.Commit(context.Background(), exchangeID, []effect.Effect{
		effect.Compatibility{
			Feature: feature,
			Outcome: outcome,
			Subject: subject,
		},
	}); err != nil {
		return canonical.InternalError("compatibility effect sink commit failed")
	}
	return nil
}

func chatCompletionsToolSubject(msgIdx int, toolIdx int, field string) compat.Subject {
	field = strings.TrimSpace(field) // swobu:io-string source=boundary
	if field == "" {
		return ""
	}
	return compat.Subject("wire:/messages/" + strconv.Itoa(msgIdx) + "/tool_calls/" + strconv.Itoa(toolIdx) + "/" + field)
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
