package chatcompletions

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/metrofun/swobu/internal/adapters/outbound/protocols"
	"github.com/metrofun/swobu/internal/domain/compatibility"
)

func Realize(request compatibility.CanonicalRequest, deliveryMode compatibility.DeliveryMode) (protocols.WireRequest, error) {
	switch typed := request.(type) {
	case compatibility.DialogCanonicalRequest:
		return Encode(typed, deliveryMode)
	case compatibility.GenerationCanonicalRequest:
		thread := typed.Thread()
		if len(thread) == 0 {
			return protocols.WireRequest{}, compatibility.BadRequest("response request does not contain replayable conversation input")
		}
		return Encode(compatibility.NewDialogRequest(typed.Model(), thread), deliveryMode)
	default:
		return protocols.WireRequest{}, compatibility.UnsupportedOperation("chat completions protocol does not implement the canonical semantic request")
	}
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

func Encode(req compatibility.DialogCanonicalRequest, deliveryMode compatibility.DeliveryMode) (protocols.WireRequest, error) {
	switch deliveryMode {
	case compatibility.DeliveryModeBuffered, compatibility.DeliveryModeStreaming:
	default:
		return protocols.WireRequest{}, compatibility.UnsupportedDelivery("conversation requests do not implement the requested delivery mode on the chat completions protocol")
	}

	items := req.Items()
	wireMessages, err := encodeItems(items)
	if err != nil {
		return protocols.WireRequest{}, err
	}

	raw, err := json.Marshal(requestBody{
		Model:    req.Model(),
		Messages: wireMessages,
		Stream:   deliveryMode == compatibility.DeliveryModeStreaming,
	})
	if err != nil {
		return protocols.WireRequest{}, compatibility.BadRequest("conversation request could not be encoded for the chat completions protocol")
	}

	return protocols.WireRequest{
		Method:  http.MethodPost,
		Path:    "/chat/completions",
		Body:    bytes.NewReader(raw),
		HasBody: true,
	}, nil
}

func encodeItems(items []compatibility.CanonicalItem) ([]messageBody, error) {
	out := make([]messageBody, 0, len(items))
	for i := 0; i < len(items); {
		item := items[i]
		if item.Kind == compatibility.ItemKindToolResult {
			if strings.TrimSpace(item.ToolUseID) == "" {
				return nil, compatibility.BadRequest("tool_result items require tool_use_id for the chat completions protocol")
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
			if current.Kind == compatibility.ItemKindToolResult || roleForChatItem(current) != role {
				break
			}
			switch current.Kind {
			case compatibility.ItemKindText:
				text += current.Text
			case compatibility.ItemKindToolUse:
				args, err := json.Marshal(current.Input)
				if err != nil {
					return nil, compatibility.BadRequest("tool_use input could not be encoded for the chat completions protocol")
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
				return nil, compatibility.UnsupportedOperation("canonical item is not supported on the chat completions protocol")
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

func roleForChatItem(item compatibility.CanonicalItem) string {
	switch item.Author {
	case compatibility.ItemAuthorAssistant:
		return "assistant"
	case compatibility.ItemAuthorTool:
		return "tool"
	default:
		return "user"
	}
}
