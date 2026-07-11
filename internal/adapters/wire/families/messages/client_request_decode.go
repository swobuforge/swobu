package messages

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
	var dto messagesRequestDTO
	if err := json.Unmarshal(raw, &dto); err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), canonical.BadRequest("messages request body is invalid JSON")
	}
	if len(dto.Messages) == 0 {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), canonical.BadRequest("messages request is missing required fields")
	}
	streamRequested, err := core.DecodeRequestStreamFlag(raw, "messages")
	if err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	items := make([]canonical.CanonicalItem, 0, len(dto.Messages))
	for idx, msg := range dto.Messages {
		decoded, err := decodeMessagesItems(msg.Content, idx, strings.TrimSpace(msg.Role)) // swobu:io-string source=boundary
		if err != nil {
			return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
		}
		items = append(items, decoded...)
	}
	resolvedDelivery := delivery.BufferedDelivery()
	if streamRequested {
		resolvedDelivery = delivery.StreamingDelivery(delivery.FramingNone)
	}
	return canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: strings.TrimSpace(dto.Model), // swobu:io-string source=boundary
		Items: items,
	}), resolvedDelivery, nil
}

func decodeMessagesItems(raw json.RawMessage, msgIdx int, role string) ([]canonical.CanonicalItem, error) {
	_ = msgIdx
	if role == "" {
		role = "user"
	}
	author := openaicompat.AuthorForRole(role)
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		if text != "" {
			return []canonical.CanonicalItem{canonical.NewTextItem(author, text)}, nil
		}
		return nil, canonical.BadRequest("messages request content must not be empty")
	}
	var parts []messagesContentPartDTO
	if err := json.Unmarshal(raw, &parts); err != nil {
		return nil, canonical.BadRequest("messages request content is invalid")
	}
	if len(parts) == 0 {
		return nil, canonical.BadRequest("messages request content must not be empty")
	}
	decoded := make([]canonical.CanonicalItem, 0, len(parts))
	for _, part := range parts {
		partType := strings.TrimSpace(part.Type) // swobu:io-string source=provider-wire
		switch partType {
		case "text":
			if part.Text == "" {
				return nil, canonical.BadRequest("messages request text parts must not be empty")
			}
			decoded = append(decoded, canonical.NewTextItem(author, part.Text))
		case "tool_use":
			if strings.TrimSpace(part.Name) == "" { // swobu:io-string source=boundary
				return nil, canonical.BadRequest("messages request tool_use parts require a name")
			}
			input, err := sse.DecodeJSONObject(part.Input, "messages request tool_use input is invalid")
			if err != nil {
				return nil, err
			}
			args, err := json.Marshal(input)
			if err != nil {
				return nil, canonical.BadRequest("messages request tool_use input is invalid")
			}
			decoded = append(decoded, canonical.NewToolUseItem(author, "", strings.TrimSpace(part.ID), strings.TrimSpace(part.Name), canonical.NewToolArgumentsObject(string(args)))) // swobu:io-string source=boundary
		case "tool_result":
			if strings.TrimSpace(part.ToolUseID) == "" { // swobu:io-string source=boundary
				return nil, canonical.BadRequest("messages request tool_result parts require tool_use_id")
			}
			text, err := decodeToolResultText(part.Content)
			if err != nil {
				return nil, err
			}
			decoded = append(decoded, canonical.NewToolResultItem(author, strings.TrimSpace(part.ToolUseID), text)) // swobu:io-string source=boundary
		default:
			return nil, canonical.BadRequest("messages request content contains an unsupported part type")
		}
	}
	return decoded, nil
}

func decodeToolResultText(raw json.RawMessage) (string, error) {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text, nil
	}
	var parts []messagesTextPartDTO
	if err := json.Unmarshal(raw, &parts); err != nil {
		return "", canonical.BadRequest("messages request tool_result content is invalid")
	}
	var builder strings.Builder
	for _, part := range parts {
		if strings.TrimSpace(part.Type) != "text" { // swobu:io-string source=boundary
			return "", canonical.BadRequest("messages request tool_result content must contain text parts only")
		}
		builder.WriteString(part.Text)
	}
	return builder.String(), nil
}
