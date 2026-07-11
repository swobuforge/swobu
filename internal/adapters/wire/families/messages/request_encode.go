package messages

import (
	"encoding/json"
	"strings"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

const defaultMessagesMaxTokens = 256

type messageBody struct {
	Role    string      `json:"role"`
	Content []contentID `json:"content"`
}

type contentID struct {
	Type      string         `json:"type"`
	Text      string         `json:"text,omitempty"`
	ID        string         `json:"id,omitempty"`
	Name      string         `json:"name,omitempty"`
	Input     map[string]any `json:"input,omitempty"`
	ToolUseID string         `json:"tool_use_id,omitempty"`
	Content   string         `json:"content,omitempty"`
}

func EncodeCarrier(req canonical.CanonicalRequest, d delivery.Delivery) (carrier.WireDocument, error) {
	switch d.Mode {
	case delivery.Buffered, delivery.Streaming:
	default:
		return carrier.WireDocument{}, canonical.UnsupportedDelivery("conversation requests do not implement the requested delivery mode on the messages protocol")
	}
	wireMessages, err := encodeItems(req.Items())
	if err != nil {
		return carrier.WireDocument{}, err
	}
	payload := map[string]any{
		"model":      req.Model(),
		"messages":   wireMessages,
		"max_tokens": defaultMessagesMaxTokens,
	}
	if d.Mode == delivery.Streaming {
		payload["stream"] = true
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return carrier.WireDocument{}, canonical.BadRequest("conversation request could not be encoded for the messages protocol")
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
	if len(items) == 0 {
		return nil, canonical.BadRequest("messages protocol requires at least one canonical item")
	}
	out := make([]messageBody, 0, len(items))
	for i := 0; i < len(items); {
		role := roleForMessagesItem(items[i])
		content := make([]contentID, 0, 1)
		for i < len(items) && roleForMessagesItem(items[i]) == role {
			current := items[i]
			switch current.Kind {
			case canonical.ItemKindText:
				content = append(content, contentID{
					Type: "text",
					Text: current.Text,
				})
			case canonical.ItemKindToolUse:
				input, err := decodeToolArgumentsObject(current.Input)
				if err != nil {
					return nil, err
				}
				content = append(content, contentID{
					Type:  "tool_use",
					ID:    strings.TrimSpace(current.ToolUseID), // swobu:io-string source=boundary
					Name:  strings.TrimSpace(current.Name),      // swobu:io-string source=boundary
					Input: input,
				})
				if strings.TrimSpace(content[len(content)-1].Name) == "" { // swobu:io-string source=boundary
					return nil, canonical.BadRequest("messages protocol tool_use items require a name")
				}
			case canonical.ItemKindToolResult:
				if strings.TrimSpace(current.ToolUseID) == "" { // swobu:io-string source=boundary
					return nil, canonical.BadRequest("messages protocol tool_result items require tool_use_id")
				}
				content = append(content, contentID{
					Type:      "tool_result",
					ToolUseID: strings.TrimSpace(current.ToolUseID), // swobu:io-string source=boundary
					Content:   current.Text,
				})
			default:
				return nil, canonical.UnsupportedOperation("canonical item is not supported on the messages protocol")
			}
			i++
		}
		if len(content) == 0 {
			continue
		}
		out = append(out, messageBody{
			Role:    role,
			Content: content,
		})
	}
	return out, nil
}

func roleForMessagesItem(item canonical.CanonicalItem) string {
	switch item.Author {
	case canonical.ItemAuthorAssistant:
		return "assistant"
	default:
		return "user"
	}
}

func decodeToolArgumentsObject(input canonical.ToolArguments) (map[string]any, error) {
	raw := input.RawObject()
	trimmedRaw := strings.TrimSpace(raw) // swobu:io-string source=boundary
	if trimmedRaw == "" {
		return map[string]any{}, nil
	}
	out := map[string]any{}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, canonical.BadRequest("messages protocol tool_use input must be a JSON object")
	}
	return out, nil
}
