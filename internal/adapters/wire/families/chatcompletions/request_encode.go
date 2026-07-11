package chatcompletions

import (
	"encoding/json"
	"strings"

	"github.com/swobuforge/swobu/internal/carrier"
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

	payload := map[string]any{
		"model":    req.Model(),
		"messages": wireMessages,
	}
	if d.Mode == delivery.Streaming {
		payload["stream"] = true
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return carrier.WireDocument{}, canonical.BadRequest("conversation request could not be encoded for the chat completions protocol")
	}

	return carrier.NewWireDocument(
		carrier.StageProviderRequestOut,
		"",
		"application/json",
		nil,
		raw,
		carrier.Meta{},
	), nil
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
				args := current.Input.RawObject()
				toolCalls = append(toolCalls, toolCallBody{
					ID:   current.ToolUseID,
					Type: "function",
					Function: toolFunctionBody{
						Name:      current.Name,
						Arguments: args,
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
