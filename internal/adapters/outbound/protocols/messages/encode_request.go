package messages

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/swobuforge/swobu/internal/adapters/outbound/protocols"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

const defaultMessagesMaxTokens = 256

func EncodeRequest(request canonical.CanonicalRequest, deliveryMode bool) (protocols.WireRequest, error) {
	switch typed := request.(type) {
	case canonical.DialogCanonicalRequest:
		return Encode(typed, deliveryMode)
	case canonical.GenerationCanonicalRequest:
		thread := typed.Thread()
		if len(thread) == 0 {
			return protocols.WireRequest{}, canonical.BadRequest("response request does not contain replayable conversation input")
		}
		return Encode(canonical.NewDialogRequest(typed.Model(), thread), deliveryMode)
	default:
		return protocols.WireRequest{}, canonical.UnsupportedOperation("messages protocol does not implement the canonical semantic request")
	}
}

type requestBody struct {
	Model     string        `json:"model"`
	Messages  []messageBody `json:"messages"`
	MaxTokens int           `json:"max_tokens"`
	Stream    bool          `json:"stream,omitempty"`
}

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

func Encode(req canonical.DialogCanonicalRequest, deliveryMode bool) (protocols.WireRequest, error) {
	switch deliveryMode {
	case false, true:
	default:
		return protocols.WireRequest{}, canonical.UnsupportedDelivery("conversation requests do not implement the requested delivery variant on the messages protocol")
	}
	wireMessages, err := encodeItems(req.Items())
	if err != nil {
		return protocols.WireRequest{}, err
	}
	raw, err := json.Marshal(requestBody{
		Model:     req.Model(),
		Messages:  wireMessages,
		MaxTokens: defaultMessagesMaxTokens,
		Stream:    deliveryMode == true,
	})
	if err != nil {
		return protocols.WireRequest{}, canonical.BadRequest("conversation request could not be encoded for the messages protocol")
	}
	return protocols.WireRequest{
		Method:  http.MethodPost,
		Path:    "/messages",
		Body:    bytes.NewReader(raw),
		HasBody: true,
	}, nil
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
				content = append(content, contentID{
					Type:  "tool_use",
					ID:    strings.TrimSpace(current.ToolUseID), // swobu:io-string source=boundary
					Name:  strings.TrimSpace(current.Name),      // swobu:io-string source=boundary
					Input: cloneInput(current.Input),
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

func cloneInput(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}
