package messages

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/wire"
	sse "github.com/swobuforge/swobu/internal/wire/framing/sse"
	openaiwire "github.com/swobuforge/swobu/internal/wire/openai"
	core "github.com/swobuforge/swobu/internal/wire/primitives"
	shared "github.com/swobuforge/swobu/internal/wire/shared"
)

type messagesImageSourceType string

const (
	messagesImageSourceURL    messagesImageSourceType = "url"
	messagesImageSourceBase64 messagesImageSourceType = "base64"
)

func (decoder ClientRequestDecoder) DecodeClientRequest(doc carrier.Document) (wire.ClientDecodeResult, error) {
	var dto messagesRequestDTO
	if err := sse.DecodePermissiveJSON(doc.RawBytes(), &dto, "messages request", nil); err != nil {
		return wire.ClientDecodeResult{}, err
	}
	value, decisions, err := shared.WithAccumulatedDecisions(func(sink compat.Sink) (wire.ClientRequestResult, error) {
		request, delivery, err := decoder.decodeClientRequestDTOWithDecisions(dto, doc.RawBytes(), sink, "")
		if err != nil {
			return wire.ClientRequestResult{}, err
		}
		// Explicit predecessor selection makes the entire supplied messages
		// array the current contribution. Role morphology must not partition it
		// into a second, competing predecessor.
		if strings.TrimSpace(dto.PreviousResponseWireID) != "" { // swobu:io-string source=boundary
			requestFingerprint, err := fingerprintMessagesRequest(dto.Messages)
			return wire.ClientRequestResult{
				Request: request, Delivery: delivery, RequestFingerprint: requestFingerprint,
			}, err
		}
		history, err := fingerprintMessagesHistory(dto.Messages)
		if err != nil {
			return wire.ClientRequestResult{}, err
		}
		result := wire.ClientRequestResult{Request: request, Delivery: delivery, RequestFingerprint: history.request}
		if history.previous != nil {
			rebasedDTO := dto
			rebasedDTO.Messages = history.current
			raw, err := json.Marshal(rebasedDTO)
			if err != nil {
				return wire.ClientRequestResult{}, err
			}
			rebased, _, err := decoder.decodeClientRequestDTOWithDecisions(rebasedDTO, raw, nil, "")
			if err != nil {
				return wire.ClientRequestResult{}, err
			}
			result.RebasedRequest = &wire.RebasedRequest{Previous: *history.previous, Request: rebased}
		}
		return result, nil
	})
	return wire.ClientDecodeResult{Request: value, Decisions: decisions}, err
}

func (decoder ClientRequestDecoder) decodeClientRequestWithDecisions(doc carrier.Document, sink compat.Sink, exchangeID string) (canonical.CanonicalRequest, delivery.Delivery, error) {
	raw := doc.RawBytes()
	var dto messagesRequestDTO
	if err := sse.DecodePermissiveJSON(raw, &dto, "messages request", nil); err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	return decoder.decodeClientRequestDTOWithDecisions(dto, raw, sink, exchangeID)
}

func (decoder ClientRequestDecoder) decodeClientRequestDTOWithDecisions(dto messagesRequestDTO, raw []byte, sink compat.Sink, exchangeID string) (canonical.CanonicalRequest, delivery.Delivery, error) {
	if len(dto.Messages) == 0 {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), canonical.BadRequest("messages request is missing required fields")
	}
	instructions, err := decodeMessagesSystem(dto.System, decoder.ImageLimits)
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
		decoded, nextPending, err := decodeMessagesItems(msg.Content, idx, strings.TrimSpace(msg.Role), tools, pendingToolUseIDs, decoder.ImageLimits) // swobu:io-string source=boundary
		if err != nil {
			return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
		}
		items = append(items, decoded...)
		pendingToolUseIDs = nextPending
	}
	if err := shared.ValidateImageDecodeLimits(items, decoder.ImageLimits); err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), canonical.BadRequest("messages request image limits are exceeded")
	}
	controls, err := decodeMessagesGenerationControls(dto)
	if err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	var previousResponse *canonical.ResponseRef
	previousResponseID := canonical.NewSwobuResponseID(dto.PreviousResponseWireID)
	if dto.PreviousResponseWireID != "" && previousResponseID.IsZero() { // swobu:io-string source=boundary
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), canonical.BadRequest("previous_response_id is empty")
	}
	if !previousResponseID.IsZero() {
		previousResponse = &canonical.ResponseRef{SwobuID: previousResponseID}
	}
	resolvedDelivery := delivery.BufferedDelivery()
	if streamRequested {
		resolvedDelivery = delivery.StreamingDelivery(delivery.FramingNone)
	}
	toolSet, err := canonical.NewToolSet(tools)
	if err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), canonical.BadRequest("messages tools are invalid")
	}
	params := canonical.RequestParams{
		Model:            canonical.Specify(strings.TrimSpace(dto.Model)), // swobu:io-string source=boundary
		Items:            items,
		Controls:         controls,
		PreviousResponse: previousResponse,
	}
	if len(dto.System) > 0 {
		params.Instructions = canonical.Specify(canonical.NewSystemInstructionSet(instructions))
	}
	if dto.Tools != nil {
		params.Tools = canonical.Specify(toolSet)
	}
	if len(dto.ToolChoice) > 0 {
		params.ToolPolicy = canonical.Specify(toolPolicy)
	}
	if len(dto.DisableParallelToolUse) > 0 {
		params.ToolCallBatch = canonical.Specify(toolCallBatch)
	}
	return canonical.NewCanonicalRequest(params), resolvedDelivery, nil
}

func decodeMessagesSystem(raw json.RawMessage, imageLimits shared.ImageDecodeLimitPolicy) (string, error) {
	trimmed := strings.TrimSpace(string(raw)) // swobu:io-string source=boundary
	if trimmed == "" || trimmed == "null" {
		return "", nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return strings.TrimSpace(text), nil // swobu:io-string source=boundary
	}
	parts, err := openaiwire.DecodeTextContentItems(raw, "messages system", canonical.MessageRoleSystem, imageLimits)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(joinMessagesText(parts)), nil // swobu:io-string source=boundary
}

func joinMessagesText(items []canonical.CanonicalItem) string {
	var builder strings.Builder
	for _, item := range items {
		if message, ok := item.Message(); ok {
			for _, part := range message.Content() {
				if text, ok := part.Text(); ok {
					builder.WriteString(text.Text())
				}
			}
		}
	}
	return builder.String()
}

// swobu:lint ignore function-complexity because=Messages item decoding keeps all content-block variants at one boundary seam.
func decodeMessagesItems(raw json.RawMessage, msgIdx int, role string, tools []canonical.ToolDeclaration, pendingToolUseIDs []string, imageLimits shared.ImageDecodeLimitPolicy) ([]canonical.CanonicalItem, []string, error) {
	if role == "" {
		role = "user"
	}
	author := openaiwire.AuthorForRole(role)
	parts, err := openaiwire.DecodeContentParts(raw, "messages request content is invalid")
	if err != nil {
		return nil, pendingToolUseIDs, err
	}
	if len(parts) == 0 {
		return nil, pendingToolUseIDs, canonical.BadRequest("messages request content must not be empty")
	}
	decoded := make([]canonical.CanonicalItem, 0, len(parts))
	pending := append([]string(nil), pendingToolUseIDs...)
	messageParts := make([]canonical.MessagePart, 0)
	flushMessage := func() error {
		if len(messageParts) == 0 {
			return nil
		}
		message, err := canonical.NewMessageItem(author, messageParts)
		if err != nil {
			return canonical.BadRequest("messages request message item is invalid")
		}
		decoded = append(decoded, message)
		messageParts = nil
		return nil
	}
	err = openaiwire.WalkContentParts(parts, func(partIdx int, part openaiwire.ContentPartItem) error {
		partType := strings.TrimSpace(part.Type) // swobu:io-string source=provider-wire
		switch partType {
		case "text":
			if part.Text == "" {
				return canonical.BadRequest("messages request text parts must not be empty")
			}
			messageParts = append(messageParts, canonical.NewTextMessagePart(part.Text))
		case "image":
			if author != canonical.MessageRoleUser {
				return canonical.BadRequest("messages request image input is only valid in user messages")
			}
			image, err := decodeMessagesImageSource(part.Source, imageLimits)
			if err != nil {
				return canonical.BadRequest("messages request image source is invalid")
			}
			messageParts = append(messageParts, canonical.NewImageMessagePart(image))
		case "tool_use":
			if err := flushMessage(); err != nil {
				return err
			}
			input, err := canonical.ParseJSONObject(part.Input)
			if err != nil {
				return err
			}
			name := strings.TrimSpace(part.Name) // swobu:io-string source=boundary
			if name == "" {
				return canonical.BadRequest("messages request tool_use parts require a name")
			}
			toolUseID := strings.TrimSpace(part.ID) // swobu:io-string source=boundary
			if toolUseID == "" {
				toolUseID = openaiwire.GeneratedToolUseID(msgIdx, partIdx)
			}
			pending = append(pending, toolUseID)
			toolKey, err := canonical.ResolveHistoricalToolKeyByName(tools, name, canonical.ToolKindFunction)
			if err != nil {
				return canonical.BadRequest("messages request tool_use has an invalid tool identity")
			}
			callID, _ := canonical.NewToolCallID(toolUseID)
			item, err := canonical.NewToolCallItem(callID, toolKey, canonical.NewJSONObjectToolInput(input))
			if err != nil {
				return canonical.BadRequest("messages request tool_use is invalid")
			}
			decoded = append(decoded, item)
		case "tool_result":
			if err := flushMessage(); err != nil {
				return err
			}
			toolUseID := strings.TrimSpace(part.ToolUseID) // swobu:io-string source=boundary
			if toolUseID == "" {
				return canonical.BadRequest("messages request tool_result parts require tool_use_id")
			}
			content, err := decodeToolResultContent(part.Content, imageLimits)
			if err != nil {
				return err
			}
			callID, _ := canonical.NewToolCallID(toolUseID)
			result, err := canonical.NewToolResultItem(callID, content, part.IsError)
			if err != nil {
				return canonical.BadRequest("messages request tool_result is invalid")
			}
			decoded = append(decoded, result)
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
	if err := flushMessage(); err != nil {
		return nil, pending, err
	}
	return decoded, pending, nil
}

func decodeToolResultContent(raw json.RawMessage, imageLimits shared.ImageDecodeLimitPolicy) ([]canonical.ToolResultPart, error) {
	parts, err := openaiwire.DecodeContentParts(raw, "messages request tool_result content is invalid")
	if err != nil {
		return nil, err
	}
	content := make([]canonical.ToolResultPart, 0, len(parts))
	err = openaiwire.WalkContentParts(parts, func(_ int, part openaiwire.ContentPartItem) error {
		partType := strings.TrimSpace(part.Type) // swobu:io-string source=boundary
		if partType == "" {
			partType = "text"
		}
		switch partType {
		case "text":
			content = append(content, canonical.NewTextToolResultPart(part.Text))
		case "image":
			image, err := decodeMessagesImageSource(part.Source, imageLimits)
			if err != nil {
				return canonical.BadRequest("messages request tool_result image source is invalid")
			}
			content = append(content, canonical.NewImageToolResultPart(image))
		default:
			return canonical.BadRequest("messages request tool_result content contains an unsupported part type")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return content, nil
}

func decodeMessagesImageSource(raw json.RawMessage, limits shared.ImageDecodeLimitPolicy) (canonical.ImagePart, error) {
	var source struct {
		Type      string `json:"type"`
		URL       string `json:"url"`
		MediaType string `json:"media_type"`
		Data      string `json:"data"`
	}
	if err := json.Unmarshal(raw, &source); err != nil {
		return canonical.ImagePart{}, err
	}
	switch messagesImageSourceType(strings.TrimSpace(source.Type)) { // swobu:io-string source=boundary
	case messagesImageSourceURL:
		return canonical.NewURLImage(source.URL, canonical.Unspecified[canonical.ImageDetail]())
	case messagesImageSourceBase64:
		data, err := shared.DecodeBase64Limited(source.Data, limits.MaxInlineBytes)
		if err != nil {
			return canonical.ImagePart{}, err
		}
		mediaType, err := shared.NormalizeImageMediaType(source.MediaType)
		if err != nil {
			return canonical.ImagePart{}, err
		}
		return canonical.NewInlineImage(mediaType, data, canonical.Unspecified[canonical.ImageDetail]())
	default:
		return canonical.ImagePart{}, fmt.Errorf("messages image source type is unsupported")
	}
}

func decodeMessagesTools(tools []messagesToolDTO, sink compat.Sink, exchangeID string) ([]canonical.ToolDeclaration, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	out := make([]canonical.ToolDeclaration, 0, len(tools))
	for _, tool := range tools {
		schema, err := messagesToolSchemaFromWire(tool.InputSchema)
		if err != nil {
			return nil, err
		}
		name := strings.TrimSpace(tool.Name) // swobu:io-string source=boundary
		if name == "" {
			return nil, canonical.BadRequest("messages request tool declarations require a name")
		}
		id, err := canonical.ToolIdentityFromWire(name, canonical.ToolKindFunction)
		if err != nil {
			return nil, err
		}
		declaration, err := canonical.NewFunctionTool(id, tool.Description, schema, canonical.Unspecified[bool]())
		if err != nil {
			return nil, err
		}
		out = append(out, declaration)
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
