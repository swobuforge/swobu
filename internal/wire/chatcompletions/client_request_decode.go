package chatcompletions

import (
	"encoding/json"
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

func (decoder ClientRequestDecoder) DecodeClientRequest(doc carrier.Document) (wire.ClientDecodeResult, error) {
	var dto chatCompletionsRequestDTO
	if err := shared.DecodeExtensibleRequestObject(doc.RawBytes(), &dto, "chat completions request"); err != nil {
		return wire.ClientDecodeResult{}, err
	}
	var changes []compat.Change
	value, err := func(changeLog *[]compat.Change) (wire.ClientRequestResult, error) {
		request, delivery, err := decoder.decodeClientRequestDTOWithChanges(dto, doc.RawBytes(), changeLog, "")
		if err != nil {
			return wire.ClientRequestResult{}, err
		}
		// An explicit selector defines the predecessor semantically. Every
		// supplied message is therefore current contribution, regardless of
		// whether its roles resemble completed implicit history.
		if strings.TrimSpace(dto.PreviousResponseWireID) != "" { // swobu:io-string source=boundary
			requestFingerprint, err := fingerprintChatCompletionsRequest(dto.Messages[chatCompletionsLeadingContextEnd(dto.Messages):])
			return wire.ClientRequestResult{
				Request: request, Delivery: delivery, RequestFingerprint: requestFingerprint,
			}, err
		}
		history, err := fingerprintChatCompletionsHistory(dto.Messages)
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
	var dto chatCompletionsRequestDTO
	if err := shared.DecodeExtensibleRequestObject(raw, &dto, "chat completions request"); err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	return decoder.decodeClientRequestDTOWithChanges(dto, raw, changeLog, exchangeID)
}

func (decoder ClientRequestDecoder) decodeClientRequestDTOWithChanges(dto chatCompletionsRequestDTO, raw []byte, changeLog *[]compat.Change, exchangeID string) (canonical.CanonicalRequest, delivery.Delivery, error) {
	if len(dto.Messages) == 0 {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), canonical.BadRequest("chat completions request is missing required fields")
	}
	tools, err := decodeChatCompletionsTools(dto.Tools, changeLog, exchangeID)
	if err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	toolPolicy, err := decodeChatCompletionsToolChoice(dto.ToolChoice, tools, changeLog, exchangeID)
	if err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	toolCallBatch, err := decodeChatCompletionsToolCallBatch(dto.ParallelToolCalls)
	if err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	streamRequested, err := core.DecodeRequestStreamFlag(raw, "chat completions")
	if err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	items, instructions, err := decodeChatConversation(dto.Messages, tools, changeLog, exchangeID, decoder.ImageLimits)
	if err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	controls, reasoning, err := decodeChatCompletionsGenerationControls(dto)
	if err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	outputFormat, err := decodeChatCompletionsOutputFormat(dto.ResponseFormat, changeLog, exchangeID)
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
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), canonical.BadRequest("chat completions tools are invalid")
	}
	contextItems := append([]canonical.CanonicalItem(nil), instructions...)
	if len(tools) > 0 {
		declarations, err := canonical.NewToolDeclarationsItem(toolSet, canonical.ContextScopeRequest)
		if err != nil {
			return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
		}
		contextItems = append(contextItems, declarations)
	}
	contextItems = append(contextItems, items...)
	params := canonical.RequestParams{
		Model:            canonical.Specify(strings.TrimSpace(dto.Model)), // swobu:io-string source=boundary
		Items:            contextItems,
		Controls:         controls,
		Reasoning:        reasoning,
		PreviousResponse: previousResponse,
	}
	if len(dto.ToolChoice) > 0 {
		params.ToolPolicy = canonical.Specify(toolPolicy)
	}
	if len(dto.ParallelToolCalls) > 0 {
		params.ToolCallBatch = canonical.Specify(toolCallBatch)
	}
	if len(dto.ResponseFormat) > 0 {
		params.OutputFormat = canonical.Specify(outputFormat)
	}
	return newChatCanonicalRequest(params, resolvedDelivery, decoder.ImageLimits)
}

func decodeChatConversation(messages []chatCompletionsMessageDTO, tools []canonical.ToolDeclaration, changeLog *[]compat.Change, exchangeID string, imageLimits shared.ImageDecodeLimitPolicy) ([]canonical.CanonicalItem, []canonical.CanonicalItem, error) {
	items := make([]canonical.CanonicalItem, 0, len(messages))
	instructions := make([]canonical.CanonicalItem, 0, 2)
	leadingInstructions := true
	for idx, msg := range messages {
		role := strings.TrimSpace(msg.Role) // swobu:io-string source=boundary
		if leadingInstructions && (role == "system" || role == "developer") {
			author := canonical.MessageRoleSystem
			if role == "developer" {
				author = canonical.MessageRoleDeveloper
			}
			textItems, err := openaiwire.DecodeTextContentItems(msg.Content, "chat completions", author, imageLimits, func(partIndex int, _ string) error {
				return appendChatOccurrenceChange(
					changeLog, exchangeID, canonical.RequestItemsKind, compat.Omission,
					canonical.RequestPartOccurrence(canonical.RequestPartRef{Item: uint32(idx), Part: uint32(partIndex)}),
				)
			})
			if err != nil {
				return nil, nil, err
			}
			instruction, err := canonical.NewScopedMessageItem(author, []canonical.MessagePart{canonical.NewTextMessagePart(joinItemText(textItems))}, canonical.ContextScopeRequest)
			if err != nil {
				return nil, nil, err
			}
			instructions = append(instructions, instruction)
			continue
		}
		leadingInstructions = false
		decoded, err := decodeChatCompletionsItems(changeLog, exchangeID, tools, msg.Role, msg.Content, msg.ToolCalls, msg.ToolCallID, idx, imageLimits)
		if err != nil {
			return nil, nil, err
		}
		items = append(items, decoded...)
	}
	return items, instructions, nil
}

func newChatCanonicalRequest(params canonical.RequestParams, resolvedDelivery delivery.Delivery, imageLimits shared.ImageDecodeLimitPolicy) (canonical.CanonicalRequest, delivery.Delivery, error) {
	if err := shared.ValidateImageDecodeLimits(params.Items, imageLimits); err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), canonical.BadRequest("chat completions request image limits are exceeded")
	}
	request := canonical.NewCanonicalRequest(params)
	return request, resolvedDelivery, nil
}

// swobu:lint ignore string-switch because=protocol boundary decodes wire tool-call kinds.
// swobu:lint ignore function-complexity because=chat completions item decoding keeps all wire item variants at one boundary seam.
func decodeChatCompletionsItems(
	changeLog *[]compat.Change,
	exchangeID string,
	tools []canonical.ToolDeclaration, role string,
	content json.RawMessage,
	toolCalls []chatCompletionsToolCallDTO,
	toolCallID string,
	msgIdx int,
	imageLimits shared.ImageDecodeLimitPolicy,
) ([]canonical.CanonicalItem, error) {
	role = strings.TrimSpace(role) // swobu:io-string source=boundary
	if role == "tool" {
		if strings.TrimSpace(toolCallID) == "" { // swobu:io-string source=boundary
			if err := appendChatOccurrenceChange(changeLog, exchangeID, canonical.RequestItemsToolCallCallID, compat.Approximation, chatCompletionsToolSubject(msgIdx, 0, "tool_call_id")); err != nil {
				return nil, err
			}
			return nil, canonical.BadRequest("chat completions tool messages require tool_call_id")
		}
		callID, _ := canonical.NewToolCallID(toolCallID)
		parts, err := decodeChatCompletionsTextParts(content, imageLimits, func(partIndex int, _ string) error {
			return appendChatOccurrenceChange(
				changeLog, exchangeID, canonical.RequestItemsKind, compat.Omission,
				canonical.RequestPartOccurrence(canonical.RequestPartRef{Item: uint32(msgIdx), Part: uint32(partIndex)}),
			)
		})
		if err != nil {
			return nil, err
		}
		result, err := canonical.NewToolResultItem(callID, parts, false)
		if err != nil {
			return nil, canonical.BadRequest("chat completions tool result is invalid")
		}
		return []canonical.CanonicalItem{result}, nil
	}
	author := openaiwire.AuthorForRole(role)
	if role == "system" {
		author = canonical.MessageRoleSystem
	} else if role == "developer" {
		author = canonical.MessageRoleDeveloper
	}
	textItems, err := openaiwire.DecodeTextContentItems(content, "chat completions", author, imageLimits, func(partIndex int, _ string) error {
		return appendChatOccurrenceChange(
			changeLog, exchangeID, canonical.RequestItemsKind, compat.Omission,
			canonical.RequestPartOccurrence(canonical.RequestPartRef{Item: uint32(msgIdx), Part: uint32(partIndex)}),
		)
	})
	if err != nil {
		return nil, err
	}
	items := append([]canonical.CanonicalItem(nil), textItems...)
	for idx, call := range toolCalls {
		callType := strings.ToLower(strings.TrimSpace(call.Type)) // swobu:io-string source=domain
		if callType != "" && callType != canonical.ToolTypeFunction && callType != canonical.ToolTypeCustom {
			if err := appendChatOccurrenceChange(changeLog, exchangeID, canonical.RequestItemsToolCallTool, compat.Omission, chatCompletionsToolSubject(msgIdx, idx, "type")); err != nil {
				return nil, err
			}
			continue
		}
		id := strings.TrimSpace(call.ID) // swobu:io-string source=boundary
		if id == "" {
			if err := appendChatOccurrenceChange(changeLog, exchangeID, canonical.RequestItemsToolCallCallID, compat.Approximation, chatCompletionsToolSubject(msgIdx, idx, "id")); err != nil {
				return nil, err
			}
			id = openaiwire.GeneratedToolUseID(msgIdx, idx)
		}
		switch callType {
		case "", "function":
			if call.Function == nil {
				return nil, canonical.BadRequest("chat completions tool calls require a function body")
			}
			if strings.TrimSpace(call.Function.Name) == "" { // swobu:io-string source=boundary
				return nil, canonical.BadRequest("chat completions tool calls require a function name")
			}
			input, err := decodeChatCompletionsFunctionArguments(call.Function.Arguments)
			if err != nil {
				return nil, err
			}
			toolKey, err := canonical.ResolveHistoricalToolKeyByName(tools, call.Function.Name, canonical.ToolKindFunction)
			if err != nil {
				return nil, canonical.BadRequest("chat completions tool call has an invalid function identity")
			}
			callID, _ := canonical.NewToolCallID(id)
			item, err := canonical.NewToolCallItem(callID, toolKey, canonical.NewJSONObjectToolInput(input))
			if err != nil {
				return nil, canonical.BadRequest("chat completions tool call is invalid")
			}
			items = append(items, item)
		case "custom":
			if call.Custom == nil {
				return nil, canonical.BadRequest("chat completions custom tool calls require a custom body")
			}
			if strings.TrimSpace(call.Custom.Name) == "" { // swobu:io-string source=boundary
				return nil, canonical.BadRequest("chat completions custom tool calls require a custom name")
			}
			toolKey, err := canonical.ResolveHistoricalToolKeyByName(tools, call.Custom.Name, canonical.ToolKindCustom)
			if err != nil {
				return nil, canonical.BadRequest("chat completions custom tool call has an invalid identity")
			}
			callID, _ := canonical.NewToolCallID(id)
			item, err := canonical.NewToolCallItem(callID, toolKey, canonical.NewTextToolInput(call.Custom.Input))
			if err != nil {
				return nil, canonical.BadRequest("chat completions custom tool call is invalid")
			}
			items = append(items, item)
		}
	}
	return items, nil
}

// OpenCode and other OpenAI-family chat-completions bridges may stringify
// function call arguments when they reconstruct a prior tool call. Accept both
// the wire object and the stringified object, then normalize to canonical JSON.
func decodeChatCompletionsFunctionArguments(raw json.RawMessage) (canonical.JSONObject, error) {
	input, err := canonical.ParseJSONObject(raw)
	if err == nil {
		return input, nil
	}
	var stringified string
	if err := json.Unmarshal(raw, &stringified); err != nil {
		return canonical.JSONObject{}, canonical.BadRequest("chat completions tool call arguments are invalid")
	}
	trimmedStringified := strings.TrimSpace(stringified) // swobu:io-string source=boundary
	input, err = canonical.ParseJSONObject([]byte(trimmedStringified))
	if err != nil {
		return canonical.JSONObject{}, canonical.BadRequest("chat completions tool call arguments are invalid")
	}
	return input, nil
}

func appendChatOccurrenceChange(changeLog *[]compat.Change, exchangeID string, feature canonical.CapabilityPath, outcome compat.Kind, occurrence canonical.Occurrence) error {
	if changeLog == nil {
		return nil
	}
	change := compat.Change{Capability: feature, Occurrence: occurrence, Kind: outcome}
	if outcome == compat.Approximation {
		change.Preserved = feature
	}
	*changeLog = compat.AppendUnique(*changeLog, change)
	return nil
}

func chatCompletionsToolSubject(msgIdx int, _ int, _ string) canonical.Occurrence {
	return canonical.RequestItemOccurrence(uint32(msgIdx))
}

func joinItemText(items []canonical.CanonicalItem) string {
	var builder strings.Builder
	for _, item := range items {
		message, ok := item.Message()
		if !ok {
			continue
		}
		for _, part := range message.Content() {
			if text, ok := part.Text(); ok {
				builder.WriteString(text.Text())
			}
		}
	}
	return builder.String()
}

func decodeChatCompletionsTextParts(
	raw json.RawMessage,
	imageLimits shared.ImageDecodeLimitPolicy,
	onUnknown func(int, string) error,
) ([]canonical.ToolResultPart, error) {
	items, err := openaiwire.DecodeTextContentItems(raw, "chat completions", canonical.MessageRoleUser, imageLimits, onUnknown)
	if err != nil || len(items) == 0 {
		return nil, err
	}
	message, ok := items[0].Message()
	if !ok {
		return nil, canonical.BadRequest("chat completions tool result content is invalid")
	}
	content := make([]canonical.ToolResultPart, 0, len(message.Content()))
	for _, part := range message.Content() {
		text, ok := part.Text()
		if !ok {
			return nil, canonical.BadRequest("chat completions tool result content must be text")
		}
		content = append(content, canonical.NewTextToolResultPart(text.Text()))
	}
	return content, nil
}
