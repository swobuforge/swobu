package chatcompletions

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	openaiwire "github.com/swobuforge/swobu/internal/wire/openai"
)

// EncodeOptions selects exact-backend request spellings and image-detail
// compatibility behavior for Chat Completions lowering.
type EncodeOptions struct {
	MaxOutputTokensField MaxOutputTokensField
	Compatibility        compat.CompatibilityPolicy
}

type messageBody struct {
	Role       string         `json:"role"`
	Content    any            `json:"content,omitempty"`
	ToolCalls  []toolCallBody `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

type toolCallBody struct {
	ID       string            `json:"id,omitempty"`
	Type     string            `json:"type"`
	Function *toolFunctionBody `json:"function,omitempty"`
	Custom   *toolCustomBody   `json:"custom,omitempty"`
}

type toolFunctionBody struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type toolCustomBody struct {
	Name  string `json:"name"`
	Input string `json:"input"`
}

func EncodeCarrierWithDecisions(req canonical.CanonicalRequest, d delivery.Delivery, sink compat.Sink, exchangeID string, options EncodeOptions) (carrier.Document, error) {
	switch d.Mode {
	case delivery.Buffered, delivery.Streaming:
	default:
		return carrier.Document{}, canonical.UnsupportedDelivery("conversation requests do not implement the requested delivery mode on the chat completions protocol")
	}

	items := req.Items()
	tools := req.Tools()
	wireMessages, err := encodeItems(items, tools, sink, exchangeID, options.Compatibility)
	if err != nil {
		return carrier.Document{}, err
	}
	wireTools, err := encodeChatCompletionsTools(tools, sink, exchangeID)
	if err != nil {
		return carrier.Document{}, err
	}
	if d.Mode == delivery.Streaming && hasChatCompletionsCustomTools(tools) {
		return carrier.Document{}, canonical.UnsupportedDelivery("chat completions streaming does not support custom tool declarations")
	}
	choice, err := encodeChatCompletionsToolChoice(req.EffectiveToolPolicy(), tools, sink, exchangeID)
	if err != nil {
		return carrier.Document{}, err
	}

	payload := map[string]any{
		"model":    req.Model(),
		"messages": wireMessages,
	}
	if instructions := req.Instructions().Instructions(); len(instructions) > 0 {
		prefix := make([]messageBody, 0, len(instructions))
		for _, instruction := range instructions {
			prefix = append(prefix, messageBody{Role: string(instruction.Role()), Content: instruction.Text()})
		}
		payload["messages"] = append(prefix, wireMessages...)
	}
	if len(wireTools) > 0 {
		payload["tools"] = wireTools
	}
	if err := encodeChatCompletionsToolCallBatch(payload, req.ToolCallBatch(), len(tools) > 0); err != nil {
		return carrier.Document{}, err
	}
	if err := encodeChatCompletionsGenerationControls(payload, req.Controls(), options.MaxOutputTokensField); err != nil {
		return carrier.Document{}, err
	}
	if responseFormat, err := encodeChatCompletionsOutputFormat(req.OutputFormat()); err != nil {
		return carrier.Document{}, err
	} else if len(responseFormat) > 0 {
		payload["response_format"] = json.RawMessage(responseFormat)
	}
	if choice != nil {
		payload["tool_choice"] = choice
	}
	logChatCompletionsEncodeShape(req, wireMessages, choice, d)
	if d.Mode == delivery.Streaming {
		payload["stream"] = true
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return carrier.Document{}, canonical.BadRequest("conversation request could not be encoded for the chat completions protocol")
	}

	// Stage marks the carrier boundary for this wire leg; exchange path
	// selection happens above this adapter.
	return carrier.NewDocument(
		"",
		"application/json",
		nil,
		raw,
		carrier.Meta{},
	), nil
}

func logChatCompletionsEncodeShape(req canonical.CanonicalRequest, wireMessages []messageBody, choice any, d delivery.Delivery) {
	instructions := strings.TrimSpace(flattenInstructionsForChatLog(req.Instructions())) // swobu:io-string source=domain
	policy := req.EffectiveToolPolicy()
	toolChoiceMode := strings.TrimSpace(string(policy.Mode)) // swobu:io-string source=domain
	toolChoiceSpecific := ""
	if policy.Mode == canonical.ToolPolicySpecific {
		if specific, ok := policy.SpecificID(); ok {
			toolChoiceSpecific = specific.String()
		}
	}
	slog.Debug("chat completions encode",
		"component", "protocol.chat_completions",
		"event", "outbound_request_shape",
		"streaming", d.Mode == delivery.Streaming,
		"instructions_present", instructions != "",
		"instructions_bytes", len(instructions),
		"message_count", len(wireMessages),
		"tool_count", len(req.Tools()),
		"function_tool_count", chatCompletionsToolKindCount(req.Tools(), canonical.ToolTypeFunction),
		"custom_tool_count", chatCompletionsToolKindCount(req.Tools(), canonical.ToolTypeCustom),
		"tool_policy", toolChoiceMode,
		"tool_policy_specific", toolChoiceSpecific,
		"tool_choice_wired", chatCompletionsWireToolChoice(choice),
		"parallel_tool_calls", strings.TrimSpace(string(req.ToolCallBatch().Mode)), // swobu:io-string source=domain
	)
}

func flattenInstructionsForChatLog(set canonical.InstructionSet) string {
	var out strings.Builder
	for _, instruction := range set.Instructions() {
		out.WriteString(instruction.Text())
	}
	return out.String()
}

func chatCompletionsToolKindCount(tools []canonical.ToolDeclaration, kind string) int {
	count := 0
	for _, tool := range tools {
		if string(tool.Kind()) == kind {
			count++
		}
	}
	return count
}

func chatCompletionsWireToolChoice(choice any) string {
	switch v := choice.(type) {
	case nil:
		return ""
	case string:
		return v
	case map[string]any:
		if name, ok := v["name"].(string); ok {
			toolType, _ := v["type"].(string)
			toolType = strings.TrimSpace(toolType) // swobu:io-string source=boundary
			name = strings.TrimSpace(name)         // swobu:io-string source=boundary
			if toolType != "" && name != "" {
				return toolType + ":" + name
			}
			if name != "" {
				return name
			}
		}
	}
	return "object"
}

// swobu:lint ignore string-switch because=protocol boundary encodes canonical tool-call kinds.
func encodeItems(items []canonical.CanonicalItem, tools []canonical.ToolDeclaration, sink compat.Sink, exchangeID string, policy compat.CompatibilityPolicy) ([]messageBody, error) {
	out := make([]messageBody, 0, len(items))
	for i := 0; i < len(items); {
		item := items[i]
		if item.Kind() == canonical.ItemKindToolResult {
			result, ok := item.ToolResult()
			if !ok || result.CallID().IsZero() {
				return nil, canonical.InternalError("chat completions tool-result item payload is invalid")
			}
			for _, part := range result.Content() {
				if part.Kind() == canonical.PartKindImage {
					if err := emitChatImageDecision(sink, exchangeID, compat.RequestItemsToolResultImage, compat.Reject); err != nil {
						return nil, err
					}
					return nil, canonical.UnsupportedOperation("chat completions tool results do not support images")
				}
			}
			if result.IsError() {
				outcome := compat.Approx
				if policy.EffectiveMode() == compat.CompatibilityStrict {
					outcome = compat.Reject
				}
				if err := emitChatImageDecision(sink, exchangeID, compat.RequestItemsToolResultIsError, outcome); err != nil {
					return nil, err
				}
				if outcome == compat.Reject {
					return nil, canonical.UnsupportedOperation("chat completions cannot preserve tool-result error state")
				}
			}
			if len(result.Content()) > 1 {
				outcome := compat.Approx
				if policy.EffectiveMode() == compat.CompatibilityStrict {
					outcome = compat.Reject
				}
				if err := emitChatImageDecision(sink, exchangeID, compat.RequestItemsToolResultContentBoundaries, outcome); err != nil {
					return nil, err
				}
				if outcome == compat.Reject {
					return nil, canonical.UnsupportedOperation("chat completions cannot preserve tool-result content boundaries")
				}
			}
			text, err := toolResultTextOnlyContent(result.Content(), "chat completions tool results")
			if err != nil {
				return nil, err
			}
			out = append(out, messageBody{
				Role:       "tool",
				Content:    text,
				ToolCallID: result.CallID().String(),
			})
			i++
			continue
		}
		wire := messageBody{}
		if message, ok := item.Message(); ok {
			wire.Role = string(message.Role())
			content, err := encodeChatMessageContent(message.Role(), message.Content(), sink, exchangeID, policy)
			if err != nil {
				return nil, err
			}
			wire.Content = content
			i++
		} else if item.Kind() == canonical.ItemKindToolCall {
			wire.Role = "assistant"
		} else {
			return nil, canonical.UnsupportedOperation("canonical item is not supported on the chat completions protocol")
		}
		if wire.Role == "assistant" {
			for i < len(items) && items[i].Kind() == canonical.ItemKindToolCall {
				call, _ := items[i].ToolCall()
				encoded, err := encodeChatToolCall(call, tools)
				if err != nil {
					return nil, err
				}
				wire.ToolCalls = append(wire.ToolCalls, encoded)
				i++
			}
		}
		out = append(out, wire)
	}
	return out, nil
}

func encodeChatMessageContent(author canonical.MessageRole, parts []canonical.MessagePart, sink compat.Sink, exchangeID string, policy compat.CompatibilityPolicy) (any, error) {
	if len(parts) == 1 {
		if text, ok := parts[0].Text(); ok {
			return text.Text(), nil
		}
	}
	out := make([]any, 0, len(parts))
	for _, part := range parts {
		if text, ok := part.Text(); ok {
			out = append(out, map[string]any{"type": "text", "text": text.Text()})
			continue
		}
		if part.Kind() == canonical.PartKindImage {
			if author != canonical.MessageRoleUser {
				return nil, canonical.UnsupportedOperation("chat completions only accepts image input in user messages")
			}
			imagePart, _ := part.Image()
			rawURL, detail, err := openaiwire.EncodeOpenAIImageURL(imagePart)
			if err != nil {
				return nil, canonical.InternalError("canonical image source is invalid")
			}
			if detail == canonical.ImageDetailOriginal {
				if policy.EffectiveMode() == compat.CompatibilityStrict {
					if err := emitChatImageDecision(sink, exchangeID, compat.RequestItemsMessageImageDetail, compat.Reject); err != nil {
						return nil, err
					}
					return nil, canonical.UnsupportedOperation("chat completions cannot lower original image detail under strict policy")
				}
				if err := emitChatImageDecision(sink, exchangeID, compat.RequestItemsMessageImageDetail, compat.Approx); err != nil {
					return nil, err
				}
				detail = canonical.ImageDetailHigh
			}
			image := map[string]string{"url": rawURL}
			if detail != "" {
				image["detail"] = string(detail)
			}
			out = append(out, map[string]any{"type": "image_url", "image_url": image})
			continue
		}
		return nil, canonical.UnsupportedOperation("chat completions cannot lower this content kind")
	}
	return out, nil
}

func emitChatImageDecision(sink compat.Sink, exchangeID string, feature compat.Feature, outcome compat.Outcome) error {
	if sink == nil {
		return nil
	}
	if err := sink.Commit(context.Background(), exchangeID, []compat.Decision{{
		Feature: feature,
		Outcome: outcome,
		Subject: compat.Subject("canonical:" + string(feature)),
	}}); err != nil {
		return canonical.InternalError("compatibility decision sink commit failed")
	}
	return nil
}

// swobu:lint ignore string-switch because=protocol boundary encodes canonical declaration kinds into chat-completions wire variants.
func encodeChatToolCall(call canonical.ToolCallItem, _ []canonical.ToolDeclaration) (toolCallBody, error) {
	tool := call.Tool()
	name := tool.Name()
	switch tool.Kind() {
	case canonical.ToolKindFunction:
		object, ok := call.Input().Object()
		if !ok {
			return toolCallBody{}, canonical.BadRequest("chat completions function calls require object input")
		}
		return toolCallBody{ID: call.CallID().String(), Type: "function", Function: &toolFunctionBody{Name: name, Arguments: object.String()}}, nil
	case canonical.ToolKindCustom:
		text, ok := call.Input().Text()
		if !ok {
			return toolCallBody{}, canonical.BadRequest("chat completions custom calls require text input")
		}
		return toolCallBody{ID: call.CallID().String(), Type: "custom", Custom: &toolCustomBody{Name: name, Input: text}}, nil
	default:
		return toolCallBody{}, canonical.UnsupportedOperation("chat completions protocol only supports function and custom tool calls")
	}
}

func textOnlyContent(parts []canonical.MessagePart, surface string) (string, error) {
	var text strings.Builder
	for _, part := range parts {
		value, ok := part.Text()
		if !ok {
			return "", canonical.UnsupportedOperation(surface + " do not support this content kind")
		}
		text.WriteString(value.Text())
	}
	return text.String(), nil
}

func toolResultTextOnlyContent(parts []canonical.ToolResultPart, surface string) (string, error) {
	var text strings.Builder
	for _, part := range parts {
		value, ok := part.Text()
		if !ok {
			return "", canonical.UnsupportedOperation(surface + " do not support this content kind")
		}
		text.WriteString(value.Text())
	}
	return text.String(), nil
}
