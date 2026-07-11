package chatcompletions

import (
	"encoding/json"
	"strings"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func encodeRequestCarrier(request canonical.CanonicalRequest, d delivery.Delivery) (carrier.WireDocument, error) {
	thread := request.Items()
	if len(thread) == 0 {
		return carrier.WireDocument{}, canonical.BadRequest("request does not contain replayable conversation input")
	}
	return EncodeCarrier(request, d)
}

type requestBody struct {
	Model    string        `json:"model"`
	Messages []messageBody `json:"messages"`
	Stream   bool          `json:"stream,omitempty"`
}

type messageBody struct {
	Role       string         `json:"role"`
	Content    any            `json:"content,omitempty"`
	ToolCalls  []toolCallBody `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

type toolCallBody struct {
	ID       string           `json:"id,omitempty"`
	Type     string           `json:"type"`
	Function toolFunctionBody `json:"function"`
}

type toolFunctionBody struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

func EncodeCarrier(req canonical.CanonicalRequest, d delivery.Delivery) (carrier.WireDocument, error) {
	switch d.Mode {
	case delivery.Buffered, delivery.Streaming:
	default:
		return carrier.WireDocument{}, canonical.UnsupportedDelivery("conversation requests do not implement the requested delivery mode on the chat completions protocol")
	}

	items := req.Items()
	wireMessages, err := encodeItems(items)
	if err != nil {
		return carrier.WireDocument{}, err
	}

	raw, err := json.Marshal(requestBody{
		Model:    req.Model(),
		Messages: wireMessages,
		Stream:   d.Mode == delivery.Streaming,
	})
	if err != nil {
		return carrier.WireDocument{}, canonical.BadRequest("conversation request could not be encoded for the chat completions protocol")
	}

	return carrier.WireDocument{
		Leg:   carrier.LegProviderRequestOut,
		Media: "application/json",
		Raw:   raw,
	}, nil
}

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
				args, err := json.Marshal(current.Input)
				if err != nil {
					return nil, canonical.BadRequest("tool_use input could not be encoded for the chat completions protocol")
				}
				toolCalls = append(toolCalls, toolCallBody{
					ID:   current.ToolUseID,
					Type: "function",
					Function: toolFunctionBody{
						Name:      current.Name,
						Arguments: string(args),
					},
				})
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
