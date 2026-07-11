package messages

import (
	"encoding/json"
	"log/slog"
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
	if err := sse.DecodeStrictJSON(raw, &dto, "messages request"); err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	if len(dto.Messages) == 0 {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), canonical.BadRequest("messages request is missing required fields")
	}
	tools, err := decodeMessagesTools(dto.Tools)
	if err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	streamRequested, err := core.DecodeRequestStreamFlag(raw, "messages")
	if err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	items := make([]canonical.CanonicalItem, 0, len(dto.Messages))
	pendingToolUseIDs := make([]string, 0, len(dto.Messages))
	for idx, msg := range dto.Messages {
		decoded, nextPending, err := decodeMessagesItems(msg.Content, idx, strings.TrimSpace(msg.Role), pendingToolUseIDs) // swobu:io-string source=boundary
		if err != nil {
			return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
		}
		items = append(items, decoded...)
		pendingToolUseIDs = nextPending
	}
	resolvedDelivery := delivery.BufferedDelivery()
	if streamRequested {
		resolvedDelivery = delivery.StreamingDelivery(delivery.FramingNone)
	}
	return canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: strings.TrimSpace(dto.Model), // swobu:io-string source=boundary
		Items: items,
		Tools: tools,
	}), resolvedDelivery, nil
}

func decodeMessagesItems(raw json.RawMessage, msgIdx int, role string, pendingToolUseIDs []string) ([]canonical.CanonicalItem, []string, error) {
	if role == "" {
		role = "user"
	}
	author := openaicompat.AuthorForRole(role)
	parts, err := openaicompat.DecodeContentParts(raw, "messages request content is invalid")
	if err != nil {
		return nil, pendingToolUseIDs, err
	}
	if len(parts) == 0 {
		return nil, pendingToolUseIDs, canonical.BadRequest("messages request content must not be empty")
	}
	decoded := make([]canonical.CanonicalItem, 0, len(parts))
	pending := append([]string(nil), pendingToolUseIDs...)
	err = openaicompat.WalkContentParts(parts, func(partIdx int, part openaicompat.ContentPart) error {
		partType := strings.TrimSpace(part.Type) // swobu:io-string source=provider-wire
		switch partType {
		case "text":
			if part.Text == "" {
				return canonical.BadRequest("messages request text parts must not be empty")
			}
			decoded = append(decoded, canonical.NewTextItem(author, part.Text))
		case "tool_use":
			if strings.TrimSpace(part.Name) == "" { // swobu:io-string source=boundary
				return canonical.BadRequest("messages request tool_use parts require a name")
			}
			input, err := sse.DecodeJSONObject(part.Input, "messages request tool_use input is invalid")
			if err != nil {
				return err
			}
			args, err := json.Marshal(input)
			if err != nil {
				return canonical.BadRequest("messages request tool_use input is invalid")
			}
			toolUseID := strings.TrimSpace(part.ID)
			if toolUseID == "" {
				toolUseID = openaicompat.GeneratedToolUseID(msgIdx, partIdx)
			}
			pending = append(pending, toolUseID)
			decoded = append(decoded, canonical.NewToolUseItem(author, "", toolUseID, strings.TrimSpace(part.Name), canonical.NewToolArgumentsObject(string(args)))) // swobu:io-string source=boundary
		case "tool_result":
			toolUseID := strings.TrimSpace(part.ToolUseID)
			if toolUseID == "" {
				if len(pending) != 1 {
					slog.Debug("messages tool_result missing tool_use_id",
						"component", "protocol.messages",
						"event", "tool_result_missing_tool_use_id",
						"message_index", msgIdx,
						"part_index", partIdx,
						"role", role,
						"pending_count", len(pending),
						"pending_tool_use_ids", append([]string(nil), pending...),
					)
					return canonical.BadRequest("messages request tool_result parts require tool_use_id")
				}
				toolUseID = pending[0]
			}
			text, err := decodeToolResultText(part.Content)
			if err != nil {
				return err
			}
			decoded = append(decoded, canonical.NewToolResultItem(author, toolUseID, text)) // swobu:io-string source=boundary
			pending = removePendingToolUseID(pending, toolUseID)
		default:
			return canonical.BadRequest("messages request content contains an unsupported part type")
		}
		return nil
	})
	if err != nil {
		return nil, pending, err
	}
	return decoded, pending, nil
}

func decodeToolResultText(raw json.RawMessage) (string, error) {
	parts, err := openaicompat.DecodeContentParts(raw, "messages request tool_result content is invalid")
	if err != nil {
		return "", err
	}
	var builder strings.Builder
	err = openaicompat.WalkContentParts(parts, func(_ int, part openaicompat.ContentPart) error {
		partType := strings.TrimSpace(part.Type) // swobu:io-string source=boundary
		if partType == "" {
			partType = "text"
		}
		if partType != "text" { // swobu:io-string source=boundary
			return canonical.BadRequest("messages request tool_result content must contain text parts only")
		}
		builder.WriteString(part.Text)
		return nil
	})
	if err != nil {
		return "", err
	}
	return builder.String(), nil
}

func removePendingToolUseID(pending []string, toolUseID string) []string {
	if len(pending) == 0 {
		return pending
	}
	for i := len(pending) - 1; i >= 0; i-- {
		if pending[i] == toolUseID {
			return append(pending[:i], pending[i+1:]...)
		}
	}
	return pending
}
