package chatcompletions

import (
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

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

func EncodeCarrierWithDecisions(req canonical.CanonicalRequest, d delivery.Delivery, sink compat.Sink, exchangeID string, maxOutputTokensField MaxOutputTokensField) (carrier.Document, error) {
	switch d.Mode {
	case delivery.Buffered, delivery.Streaming:
	default:
		return carrier.Document{}, canonical.UnsupportedDelivery("conversation requests do not implement the requested delivery mode on the chat completions protocol")
	}

	items := req.Items()
	tools := req.Tools()
	wireMessages, err := encodeItems(items)
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
	choice, err := encodeChatCompletionsToolChoice(req.ToolPolicy(), tools, sink, exchangeID)
	if err != nil {
		return carrier.Document{}, err
	}

	payload := map[string]any{
		"model":    req.Model(),
		"messages": wireMessages,
	}
	if instructions := strings.TrimSpace(req.Instructions()); instructions != "" { // swobu:io-string source=boundary
		payload["messages"] = append([]messageBody{{Role: "system", Content: instructions}}, wireMessages...)
	}
	if len(wireTools) > 0 {
		payload["tools"] = wireTools
	}
	if err := encodeChatCompletionsToolCallBatch(payload, req.ToolCallBatch(), len(tools) > 0); err != nil {
		return carrier.Document{}, err
	}
	if err := encodeChatCompletionsGenerationControls(payload, req.Controls(), maxOutputTokensField); err != nil {
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
	instructions := strings.TrimSpace(req.Instructions())              // swobu:io-string source=domain
	toolChoiceMode := strings.TrimSpace(string(req.ToolPolicy().Mode)) // swobu:io-string source=domain
	toolChoiceSpecific := ""
	if req.ToolPolicy().Mode == canonical.ToolPolicySpecific {
		if specific, ok := req.ToolPolicy().SpecificID(); ok {
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

func chatCompletionsToolKindCount(tools []canonical.ToolDecl, kind string) int {
	count := 0
	for _, tool := range tools {
		if canonical.ToolDeclKind(tool) == kind {
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
			toolType = strings.TrimSpace(toolType)
			name = strings.TrimSpace(name)
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
func encodeItems(items []canonical.CanonicalItem) ([]messageBody, error) {
	out := make([]messageBody, 0, len(items))
	for i := 0; i < len(items); {
		item := items[i]
		if item.Kind == canonical.ItemKindToolResult {
			if strings.TrimSpace(item.ToolUseID) == "" { // swobu:io-string source=boundary
				return nil, canonical.BadRequest("tool_result items require tool_use_id for the chat completions protocol")
			}
			out = append(out, messageBody{
				Role:       "tool",
				Content:    item.Text,
				ToolCallID: item.ToolUseID,
			})
			i++
			continue
		}

		role := roleForChatItem(item)
		text := ""
		toolCalls := make([]toolCallBody, 0, 1)
		for i < len(items) {
			current := items[i]
			if current.Kind == canonical.ItemKindToolResult || roleForChatItem(current) != role {
				break
			}
			switch current.Kind {
			case canonical.ItemKindText:
				text += current.Text
			case canonical.ItemKindToolUse:
				args := current.Input.RawObject()
				switch strings.ToLower(strings.TrimSpace(current.ToolType)) { // swobu:io-string source=domain
				case "", canonical.ToolTypeFunction:
					toolCalls = append(toolCalls, toolCallBody{
						ID:   current.ToolUseID,
						Type: "function",
						Function: &toolFunctionBody{
							Name:      current.Name,
							Arguments: args,
						},
					})
				case canonical.ToolTypeCustom:
					toolCalls = append(toolCalls, toolCallBody{
						ID:   current.ToolUseID,
						Type: "custom",
						Custom: &toolCustomBody{
							Name:  current.Name,
							Input: args,
						},
					})
				default:
					return nil, canonical.UnsupportedOperation("chat completions protocol only supports function and custom tool uses")
				}
			default:
				return nil, canonical.UnsupportedOperation("canonical item is not supported on the chat completions protocol")
			}
			i++
		}
		wire := messageBody{Role: role}
		if text != "" {
			wire.Content = text
		}
		if len(toolCalls) > 0 {
			wire.ToolCalls = toolCalls
		}
		out = append(out, wire)
	}
	return out, nil
}

func roleForChatItem(item canonical.CanonicalItem) string {
	switch item.Author {
	case canonical.ItemAuthorAssistant:
		return "assistant"
	case canonical.ItemAuthorTool:
		return "tool"
	default:
		return "user"
	}
}
