package messages

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/effect"
	"github.com/swobuforge/swobu/internal/wire"
	sse "github.com/swobuforge/swobu/internal/wire/framing/sse"
	openaicompat "github.com/swobuforge/swobu/internal/wire/openai"
	core "github.com/swobuforge/swobu/internal/wire/primitives"
	shared "github.com/swobuforge/swobu/internal/wire/shared"
)

func (ClientRequestDecoder) DecodeClientRequest(doc carrier.CarrierDocument) (effect.Result[wire.ClientRequestResult], error) {
	return shared.WithAccumulatedEffects(func(sink effect.Sink) (wire.ClientRequestResult, error) {
		request, delivery, err := (ClientRequestDecoder{}).decodeClientRequestWithEffects(doc, sink, "")
		return wire.ClientRequestResult{
			Request:  request,
			Delivery: delivery,
		}, err
	})
}

func (ClientRequestDecoder) decodeClientRequestWithEffects(doc carrier.CarrierDocument, sink effect.Sink, exchangeID string) (canonical.CanonicalRequest, delivery.Delivery, error) {
	raw := doc.RawBytes()
	var dto messagesRequestDTO
	if err := sse.DecodePermissiveJSON(raw, &dto, "messages request", nil); err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	if len(dto.Messages) == 0 {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), canonical.BadRequest("messages request is missing required fields")
	}
	instructions, err := decodeMessagesSystem(dto.System)
	if err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	tools, err := decodeMessagesTools(dto.Tools, sink, exchangeID)
	if err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	if err := rejectMessagesStructuredOutput(dto.ResponseFormat); err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	toolPolicy, err := decodeMessagesToolChoice(dto.ToolChoice, tools, sink, exchangeID)
	if err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	toolCallBatch, err := decodeMessagesToolCallBatch(dto.DisableParallelToolUse)
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
	controls, err := decodeMessagesGenerationControls(dto)
	if err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	resolvedDelivery := delivery.BufferedDelivery()
	if streamRequested {
		resolvedDelivery = delivery.StreamingDelivery(delivery.FramingNone)
	}
	return canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:         strings.TrimSpace(dto.Model), // swobu:io-string source=boundary
		Instructions:  instructions,
		Items:         items,
		Tools:         tools,
		ToolPolicy:    toolPolicy,
		ToolCallBatch: toolCallBatch,
		Controls:      controls,
	}), resolvedDelivery, nil
}

func decodeMessagesSystem(raw json.RawMessage) (string, error) {
	trimmed := strings.TrimSpace(string(raw)) // swobu:io-string source=boundary
	if trimmed == "" || trimmed == "null" {
		return "", nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return strings.TrimSpace(text), nil // swobu:io-string source=boundary
	}
	parts, err := openaicompat.DecodeTextContentItems(raw, "messages system", canonical.ItemAuthorUser)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(joinMessagesText(parts)), nil // swobu:io-string source=boundary
}

func joinMessagesText(items []canonical.CanonicalItem) string {
	var builder strings.Builder
	for _, item := range items {
		if item.Kind == canonical.ItemKindText {
			builder.WriteString(item.Text)
		}
	}
	return builder.String()
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
	err = openaicompat.WalkContentParts(parts, func(partIdx int, part openaicompat.ContentPartItem) error {
		partType := strings.TrimSpace(part.Type) // swobu:io-string source=provider-wire
		switch partType {
		case "text":
			if part.Text == "" {
				return canonical.BadRequest("messages request text parts must not be empty")
			}
			decoded = append(decoded, canonical.NewTextItem(author, part.Text))
		case "tool_use":
			input, err := sse.DecodeJSONObject(part.Input, "messages request tool_use input is invalid")
			if err != nil {
				return err
			}
			name := strings.TrimSpace(part.Name) // swobu:io-string source=boundary
			if name == "" {
				return canonical.BadRequest("messages request tool_use parts require a name")
			}
			args, err := json.Marshal(input)
			if err != nil {
				return canonical.BadRequest("messages request tool_use input is invalid")
			}
			toolUseID := strings.TrimSpace(part.ID) // swobu:io-string source=boundary
			if toolUseID == "" {
				toolUseID = openaicompat.GeneratedToolUseID(msgIdx, partIdx)
			}
			pending = append(pending, toolUseID)
			decoded = append(decoded, canonical.NewToolUseItem(author, "", toolUseID, name, canonical.NewToolArgumentsObject(string(args))))
		case "tool_result":
			toolUseID := strings.TrimSpace(part.ToolUseID) // swobu:io-string source=boundary
			if toolUseID == "" {
				return canonical.BadRequest("messages request tool_result parts require tool_use_id")
			}
			text, err := decodeToolResultText(part.Content)
			if err != nil {
				return err
			}
			decoded = append(decoded, canonical.NewToolResultItem(author, toolUseID, text)) // swobu:io-string source=boundary
			pending = removePendingToolUseID(pending, toolUseID)
		case "":
			if len(strings.TrimSpace(string(part.CacheControl))) > 0 || len(strings.TrimSpace(string(part.CachePoint))) > 0 { // swobu:io-string source=boundary
				return nil
			}
			return canonical.BadRequest("messages request content contains an unsupported part type")
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
	err = openaicompat.WalkContentParts(parts, func(_ int, part openaicompat.ContentPartItem) error {
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

func decodeMessagesTools(tools []messagesToolDTO, sink effect.Sink, exchangeID string) ([]canonical.ToolDecl, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	out := make([]canonical.ToolDecl, 0, len(tools))
	for idx, tool := range tools {
		schema, err := messagesToolSchemaFromWire(tool.InputSchema)
		if err != nil {
			return nil, err
		}
		name := strings.TrimSpace(tool.Name) // swobu:io-string source=boundary
		if name == "" {
			return nil, canonical.BadRequest("messages request tool declarations require a name")
		}
		id, leaf, projected := canonical.ToolIdentityFromWire(name, canonical.ToolKindFunction)
		if projected {
			if err := emitMessagesToolNameNamespaceDecision(sink, exchangeID, nil, compat.Exact, compat.Subject("wire:/tools/"+fmt.Sprint(idx)+"/name")); err != nil {
				return nil, err
			}
		}
		out = append(out, canonical.NewFunctionToolDecl(id.String(), leaf, tool.Description, schema))
	}
	return out, nil
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
