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
	if err := shared.DecodeExtensibleRequestObject(doc.RawBytes(), &dto, "messages request"); err != nil {
		return wire.ClientDecodeResult{}, err
	}
	var changes []compat.Change
	value, err := func(changeLog *[]compat.Change) (wire.ClientRequestResult, error) {
		request, delivery, err := decoder.decodeClientRequestDTOWithChanges(dto, doc.RawBytes(), changeLog, "")
		if err != nil {
			return wire.ClientRequestResult{}, err
		}
		// Explicit predecessor selection makes the entire supplied messages
		// array the current contribution. Role morphology must not partition it
		// into a second, competing predecessor.
		if strings.TrimSpace(dto.PreviousResponseWireID) != "" { // swobu:io-string source=boundary
			requestFingerprint, err := fingerprintMessagesRequest(dto.Messages)
			return wire.ClientRequestResult{Request: request, Delivery: delivery, RequestFingerprint: requestFingerprint}, err
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
			rebased, _, err := decoder.decodeClientRequestDTOWithChanges(rebasedDTO, raw, nil, "")
			if err != nil {
				return wire.ClientRequestResult{}, err
			}
			result.RebasedRequest = &wire.RebasedRequest{Previous: *history.previous, Request: rebased}
		}
		return result, nil
	}(&changes)
	return wire.ClientDecodeResult{Request: value, Changes: changes}, err
}

func (decoder ClientRequestDecoder) decodeClientRequestWithChanges(doc carrier.Document, changeLog *[]compat.Change, exchangeID string) (canonical.CanonicalRequest, delivery.Delivery, error) {
	raw := doc.RawBytes()
	var dto messagesRequestDTO
	if err := shared.DecodeExtensibleRequestObject(raw, &dto, "messages request"); err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	return decoder.decodeClientRequestDTOWithChanges(dto, raw, changeLog, exchangeID)
}

func (decoder ClientRequestDecoder) decodeClientRequestDTOWithChanges(dto messagesRequestDTO, raw []byte, changeLog *[]compat.Change, exchangeID string) (canonical.CanonicalRequest, delivery.Delivery, error) {
	if len(dto.Messages) == 0 {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), canonical.BadRequest("messages request is missing required fields")
	}
	instructions, err := decodeMessagesSystem(dto.System, decoder.ImageLimits)
	if err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	tools, deferredTools, err := decodeMessagesTools(dto.Tools, changeLog, exchangeID)
	if err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	outputFormat, err := decodeMessagesOutputFormat(dto.ResponseFormat, changeLog, exchangeID)
	if err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	nativeOutputFormat, err := decodeMessagesNativeOutputFormat(func() *messagesNativeOutputFormatDTO {
		if dto.OutputConfig == nil {
			return nil
		}
		return dto.OutputConfig.Format
	}(), changeLog, exchangeID)
	if err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	if outputFormat.Kind != canonical.OutputFormatUnspecified && nativeOutputFormat.Kind != canonical.OutputFormatUnspecified {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), canonical.BadRequest("messages request specifies structured output twice")
	}
	if nativeOutputFormat.Kind != canonical.OutputFormatUnspecified {
		outputFormat = nativeOutputFormat
	}
	toolPolicy, err := decodeMessagesToolChoice(dto.ToolChoice, tools, changeLog, exchangeID)
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
		decoded, nextPending, err := decodeMessagesItems(msg.Content, idx, strings.TrimSpace(msg.Role), tools, pendingToolUseIDs, decoder.ImageLimits, changeLog, exchangeID) // swobu:io-string source=boundary
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
	reasoning, err := decodeMessagesReasoning(dto.Thinking)
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
		Controls:         controls,
		Reasoning:        reasoning,
		PreviousResponse: previousResponse,
	}
	if len(dto.System) > 0 {
		directive, err := canonical.NewScopedMessageItem(canonical.MessageRoleSystem, []canonical.MessagePart{canonical.NewTextMessagePart(instructions)}, canonical.ContextScopeRequest)
		if err != nil {
			return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
		}
		params.Items = append(params.Items, directive)
	}
	if len(tools) > 0 {
		visibility, err := canonical.NewToolVisibilityRefinements(toolSet, deferredTools)
		if err != nil {
			return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), canonical.BadRequest("messages deferred tools are invalid")
		}
		declarations, err := canonical.NewToolDeclarationsItemWithVisibility(toolSet, canonical.ContextScopeRequest, visibility)
		if err != nil {
			return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
		}
		params.Items = append(params.Items, declarations)
	}
	params.Items = append(params.Items, items...)
	if len(dto.ToolChoice) > 0 {
		params.ToolPolicy = canonical.Specify(toolPolicy)
	}
	if len(dto.DisableParallelToolUse) > 0 {
		params.ToolCallBatch = canonical.Specify(toolCallBatch)
	}
	if len(dto.ResponseFormat) > 0 || dto.OutputConfig != nil && dto.OutputConfig.Format != nil {
		params.OutputFormat = canonical.Specify(outputFormat)
	}
	request := canonical.NewCanonicalRequest(params)
	return request, resolvedDelivery, nil
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
	parts, err := openaiwire.DecodeTextContentItems(raw, "messages system", canonical.MessageRoleSystem, imageLimits, nil)
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
func decodeMessagesItems(raw json.RawMessage, msgIdx int, role string, tools []canonical.ToolDeclaration, pendingToolUseIDs []string, imageLimits shared.ImageDecodeLimitPolicy, changeLog *[]compat.Change, exchangeID string) ([]canonical.CanonicalItem, []string, error) {
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
			var citations []messagesCitationDTO
			if len(part.Citations) > 0 && string(part.Citations) != "null" {
				if err := json.Unmarshal(part.Citations, &citations); err != nil {
					return canonical.BadRequest("messages request text citations are invalid")
				}
			}
			messagePart, err := decodeMessagesCitedText(part.Text, citations, messagesProjectionEvidence{
				feature: canonical.RequestItemsKind, changeLog: changeLog, exchangeID: exchangeID,
				occurrence: canonical.RequestPartOccurrence(canonical.RequestPartRef{Item: uint32(msgIdx), Part: uint32(partIdx)}),
			})
			if err != nil {
				return canonical.BadRequest("messages request text citations are invalid")
			}
			messageParts = append(messageParts, messagePart)
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
		case "server_tool_use":
			name := strings.TrimSpace(part.Name) // swobu:io-string source=boundary
			if name == toolSearchRegexName || name == toolSearchNaturalLanguageName {
				if author != canonical.MessageRoleAssistant {
					return canonical.BadRequest("messages server_tool_use blocks require assistant role")
				}
				if err := flushMessage(); err != nil {
					return err
				}
				toolUseID := strings.TrimSpace(part.ID)
				if toolUseID == "" {
					return canonical.BadRequest("messages server_tool_use parts require an id")
				}
				if _, err := messagesProviderDiscoveryFromDeclarations(tools, name); err != nil {
					return canonical.BadRequest("messages server_tool_use does not match a declared discovery tool")
				}
				input, err := canonical.ParseJSONObject(part.Input)
				if err != nil {
					return canonical.BadRequest("messages server_tool_use input is invalid")
				}
				callID, _ := canonical.NewToolCallID(toolUseID)
				item, err := canonical.NewToolDiscoveryCallItem(callID, canonical.NewJSONObjectToolInput(input), canonical.DiscoveryExecutorProvider)
				if err != nil {
					return canonical.BadRequest("messages server_tool_use is invalid")
				}
				decoded = append(decoded, item)
				pending = append(pending, toolUseID)
				return nil
			}
			if name != "web_search" {
				return appendMessagesOccurrenceChange(changeLog, exchangeID, canonical.RequestItemsKind, compat.Omission, canonical.RequestPartOccurrence(canonical.RequestPartRef{Item: uint32(msgIdx), Part: uint32(partIdx)}))
			}
			if author != canonical.MessageRoleAssistant {
				return canonical.BadRequest("messages server_tool_use blocks require assistant role")
			}
			if err := flushMessage(); err != nil {
				return err
			}
			toolUseID := strings.TrimSpace(part.ID) // swobu:io-string source=boundary
			if toolUseID == "" {
				return canonical.BadRequest("messages server_tool_use parts require an id")
			}
			item, err := decodeMessagesWebSearchCall(toolUseID, part.Input, messagesProjectionEvidence{
				feature: canonical.RequestItemsKind, changeLog: changeLog, exchangeID: exchangeID,
				occurrence: canonical.RequestPartOccurrence(canonical.RequestPartRef{Item: uint32(msgIdx), Part: uint32(partIdx)}),
			})
			if err != nil {
				return canonical.BadRequest("messages server_tool_use is invalid")
			}
			decoded = append(decoded, item)
			pending = append(pending, toolUseID)
		case "tool_result":
			if err := flushMessage(); err != nil {
				return err
			}
			toolUseID := strings.TrimSpace(part.ToolUseID) // swobu:io-string source=boundary
			if toolUseID == "" {
				return canonical.BadRequest("messages request tool_result parts require tool_use_id")
			}
			content, err := decodeToolResultContent(part.Content, imageLimits, msgIdx, changeLog, exchangeID)
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
		case "web_search_tool_result":
			if err := flushMessage(); err != nil {
				return err
			}
			toolUseID := strings.TrimSpace(part.ToolUseID) // swobu:io-string source=boundary
			if toolUseID == "" {
				return canonical.BadRequest("messages web_search_tool_result requires tool_use_id")
			}
			item, err := decodeMessagesWebSearchResult(toolUseID, part.Content, part.IsError, messagesProjectionEvidence{
				feature: canonical.RequestItemsKind, changeLog: changeLog, exchangeID: exchangeID,
				occurrence: canonical.RequestPartOccurrence(canonical.RequestPartRef{Item: uint32(msgIdx), Part: uint32(partIdx)}),
			})
			if err != nil {
				return canonical.BadRequest("messages web_search_tool_result is invalid")
			}
			decoded = append(decoded, item)
			pending = removePendingToolUseID(pending, toolUseID)
		case "tool_search_tool_result":
			if err := flushMessage(); err != nil {
				return err
			}
			toolUseID := strings.TrimSpace(part.ToolUseID)
			if toolUseID == "" {
				return canonical.BadRequest("messages tool_search_tool_result requires tool_use_id")
			}
			item, err := decodeMessagesClientDiscoveryResult(tools, toolUseID, part.Content)
			if err != nil {
				return canonical.BadRequest("messages tool_search_tool_result is invalid")
			}
			decoded = append(decoded, item)
			pending = removePendingToolUseID(pending, toolUseID)
		case "thinking":
			if author != canonical.MessageRoleAssistant {
				return canonical.BadRequest("messages thinking blocks require assistant role")
			}
			if err := flushMessage(); err != nil {
				return err
			}
			rawBlock, marshalErr := json.Marshal(part)
			if marshalErr != nil {
				return canonical.BadRequest("messages thinking block is invalid")
			}
			opaque, err := canonical.NewMessagesOpaqueThinking(rawBlock)
			if err != nil {
				return canonical.BadRequest("messages thinking signature is invalid")
			}
			value := part.Thinking
			if value == "" {
				value = part.Text
			}
			var reasoningParts []canonical.ReasoningPart
			if value != "" {
				reasoningPart, err := canonical.NewReasoningPart(canonical.ReasoningPartTrace, value)
				if err != nil {
					return canonical.BadRequest("messages thinking text is invalid")
				}
				reasoningParts = []canonical.ReasoningPart{reasoningPart}
			}
			item, err := canonical.NewReasoningItem(reasoningParts, opaque)
			if err != nil {
				return canonical.BadRequest("messages thinking block is invalid")
			}
			decoded = append(decoded, item)
		case "redacted_thinking":
			if author != canonical.MessageRoleAssistant {
				return canonical.BadRequest("messages redacted thinking blocks require assistant role")
			}
			if err := flushMessage(); err != nil {
				return err
			}
			rawBlock, marshalErr := json.Marshal(part)
			if marshalErr != nil {
				return canonical.BadRequest("messages redacted thinking block is invalid")
			}
			opaque, err := canonical.NewMessagesOpaqueThinking(rawBlock)
			if err != nil {
				return canonical.BadRequest("messages redacted thinking data is invalid")
			}
			item, err := canonical.NewReasoningItem(nil, opaque)
			if err != nil {
				return canonical.BadRequest("messages redacted thinking block is invalid")
			}
			decoded = append(decoded, item)
		case "":
			if len(strings.TrimSpace(string(part.CacheControl))) > 0 || len(strings.TrimSpace(string(part.CachePoint))) > 0 { // swobu:io-string source=boundary
				return nil
			}
			return canonical.BadRequest("messages request content contains an unsupported part type")
		default:
			return appendMessagesOccurrenceChange(changeLog, exchangeID, canonical.RequestItemsKind, compat.Omission, canonical.RequestPartOccurrence(canonical.RequestPartRef{Item: uint32(msgIdx), Part: uint32(partIdx)}))
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

func decodeToolResultContent(raw json.RawMessage, imageLimits shared.ImageDecodeLimitPolicy, itemIndex int, changeLog *[]compat.Change, exchangeID string) ([]canonical.ToolResultPart, error) {
	parts, err := openaiwire.DecodeContentParts(raw, "messages request tool_result content is invalid")
	if err != nil {
		return nil, err
	}
	content := make([]canonical.ToolResultPart, 0, len(parts))
	err = openaiwire.WalkContentParts(parts, func(index int, part openaiwire.ContentPartItem) error {
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
			return appendMessagesOccurrenceChange(changeLog, exchangeID, canonical.RequestItemsKind, compat.Omission, canonical.RequestPartOccurrence(canonical.RequestPartRef{Item: uint32(itemIndex), Part: uint32(index)}))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(parts) > 0 && len(content) == 0 {
		return nil, canonical.BadRequest("messages request tool_result has no surviving content")
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

func decodeMessagesTools(tools []ProviderRequestTool, changeLog *[]compat.Change, exchangeID string) ([]canonical.ToolDeclaration, []canonical.ToolKey, error) {
	if len(tools) == 0 {
		return nil, nil, nil
	}
	out := make([]canonical.ToolDeclaration, 0, len(tools))
	deferred := make([]canonical.ToolKey, 0)
	for index, tool := range tools {
		kind := strings.TrimSpace(tool.Type)
		if kind == toolSearchRegexType || kind == toolSearchNaturalLanguageType {
			if tool.DeferLoading {
				return nil, nil, canonical.BadRequest("messages discovery tool cannot be deferred")
			}
			declaration, err := decodeMessagesDiscoveryTool(tool)
			if err != nil {
				return nil, nil, err
			}
			out = append(out, declaration)
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(tool.Type), "web_search_") { // swobu:io-string source=boundary
			declaration, err := decodeMessagesWebSearchTool(tool)
			if err != nil {
				return nil, nil, err
			}
			out = append(out, declaration)
			if tool.DeferLoading {
				deferred = append(deferred, declaration.Key())
			}
			continue
		}
		if kind != "" && kind != "custom" { // swobu:io-string source=boundary
			if err := appendMessagesOccurrenceChange(changeLog, exchangeID, canonical.RequestToolsKind, compat.Omission, canonical.ToolIndexOccurrence(uint32(index))); err != nil {
				return nil, nil, err
			}
			continue
		}
		schema, err := messagesToolSchemaFromWire(tool.InputSchema)
		if err != nil {
			return nil, nil, err
		}
		name := strings.TrimSpace(tool.Name) // swobu:io-string source=boundary
		if name == "" {
			return nil, nil, canonical.BadRequest("messages request tool declarations require a name")
		}
		id, err := canonical.ToolIdentityFromWire(name, canonical.ToolKindFunction)
		if err != nil {
			return nil, nil, err
		}
		declaration, err := canonical.NewFunctionTool(id, tool.Description, schema, canonical.Unspecified[bool]())
		if err != nil {
			return nil, nil, err
		}
		out = append(out, declaration)
		if tool.DeferLoading {
			deferred = append(deferred, declaration.Key())
		}
	}
	return out, deferred, nil
}

func decodeMessagesDiscoveryTool(tool ProviderRequestTool) (canonical.ToolDeclaration, error) {
	queryKind := canonical.ToolDiscoveryQueryRegex
	schemaRaw := `{"type":"object","properties":{"pattern":{"type":"string"}},"required":["pattern"]}`
	if strings.TrimSpace(tool.Type) == toolSearchNaturalLanguageType {
		queryKind = canonical.ToolDiscoveryQueryNaturalLanguage
		schemaRaw = `{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`
	}
	object, err := canonical.ParseJSONObject([]byte(schemaRaw))
	if err != nil {
		return canonical.ToolDeclaration{}, canonical.InternalError("messages discovery schema is invalid")
	}
	return canonical.NewToolDiscoveryToolWithQuery(tool.Description, canonical.NewToolSchemaObject(object), canonical.DiscoveryExecutorProvider, queryKind)
}

func messagesProviderDiscoveryFromDeclarations(tools []canonical.ToolDeclaration, name string) (canonical.ToolDiscoveryTool, error) {
	for _, declaration := range tools {
		discovery, ok := declaration.Discovery()
		if !ok || discovery.Executor() != canonical.DiscoveryExecutorProvider {
			continue
		}
		wireName, err := messagesProviderDiscoveryName(discovery)
		if err == nil && wireName == name {
			return discovery, nil
		}
	}
	return canonical.ToolDiscoveryTool{}, canonical.BadRequest("messages discovery declaration is missing")
}

func decodeMessagesClientDiscoveryResult(tools []canonical.ToolDeclaration, rawCallID string, raw json.RawMessage) (canonical.CanonicalItem, error) {
	callID, err := canonical.NewToolCallID(rawCallID)
	if err != nil {
		return canonical.CanonicalItem{}, err
	}
	var content struct {
		Type           string `json:"type"`
		ErrorCode      string `json:"error_code"`
		ErrorMessage   string `json:"error_message"`
		ToolReferences []struct {
			Type     string `json:"type"`
			ToolName string `json:"tool_name"`
		} `json:"tool_references"`
	}
	if err := json.Unmarshal(raw, &content); err != nil {
		return canonical.CanonicalItem{}, err
	}
	if content.Type == "tool_search_tool_result_error" {
		code := canonical.Unspecified[string]()
		if strings.TrimSpace(content.ErrorCode) != "" {
			code = canonical.Specify(strings.TrimSpace(content.ErrorCode))
		}
		return canonical.NewToolDiscoveryFailureItem(callID, canonical.DiscoveryExecutorProvider, code, content.ErrorMessage)
	}
	if content.Type != "tool_search_tool_search_result" {
		return canonical.CanonicalItem{}, canonical.BadRequest("messages discovery result type is invalid")
	}
	loaded := make([]canonical.ToolDeclaration, 0, len(content.ToolReferences))
	for _, reference := range content.ToolReferences {
		declaration, err := resolveMessagesHistoricalToolReference(tools, strings.TrimSpace(reference.ToolName))
		if err != nil {
			return canonical.CanonicalItem{}, err
		}
		loaded = append(loaded, declaration)
	}
	set, err := canonical.NewToolSet(loaded)
	if err != nil {
		return canonical.CanonicalItem{}, err
	}
	return canonical.NewToolDiscoveryResultItem(callID, set, canonical.DiscoveryExecutorProvider)
}

func decodeMessagesWebSearchTool(tool ProviderRequestTool) (canonical.ToolDeclaration, error) {
	if strings.TrimSpace(tool.Type) == "web_search_" { // swobu:io-string source=boundary
		return canonical.ToolDeclaration{}, canonical.BadRequest("messages web-search version is invalid")
	}
	validateStrings := func(values []string) error {
		for _, value := range values {
			if strings.TrimSpace(value) == "" { // swobu:io-string source=boundary
				return canonical.BadRequest("messages web-search string option is empty")
			}
		}
		return nil
	}
	for _, values := range [][]string{tool.AllowedDomains, tool.BlockedDomains, tool.AllowedCallers} {
		if err := validateStrings(values); err != nil {
			return canonical.ToolDeclaration{}, err
		}
	}
	if tool.MaxUses != nil {
		if *tool.MaxUses <= 0 {
			return canonical.ToolDeclaration{}, canonical.BadRequest("messages web-search max_uses must be positive")
		}
	}
	if len(tool.UserLocation) > 0 && string(tool.UserLocation) != "null" {
		var raw map[string]any
		if err := json.Unmarshal(tool.UserLocation, &raw); err != nil {
			return canonical.ToolDeclaration{}, canonical.BadRequest("messages web-search user_location is invalid")
		}
	}
	return canonical.NewWebSearchDeclaration(), nil
}

func appendMessagesOccurrenceChange(changeLog *[]compat.Change, exchangeID string, feature canonical.CapabilityPath, outcome compat.Kind, occurrence canonical.Occurrence) error {
	if changeLog == nil {
		return nil
	}
	change := compat.Change{
		Capability: feature,
		Occurrence: occurrence,
		Kind:       outcome,
	}
	if outcome == compat.Approximation {
		change.Preserved = feature
	}
	*changeLog = compat.AppendUnique(*changeLog, change)
	return nil
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
