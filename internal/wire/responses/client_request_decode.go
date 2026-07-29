// swobu:lint ignore file-length because=Responses request decoding keeps the protocol DTO-to-canonical boundary cohesive
package responses

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/mcp"
	"github.com/swobuforge/swobu/internal/wire"
	core "github.com/swobuforge/swobu/internal/wire/primitives"
	shared "github.com/swobuforge/swobu/internal/wire/shared"
)

const (
	responsesDecodeViewFull           = "full"
	responsesDecodeViewRebasedCurrent = "rebased_current"
	responsesLiteHeader               = "x-openai-internal-codex-responses-lite"
	responsesLiteWebSocketMetadata    = "ws_request_header_x_openai_internal_codex_responses_lite"
)

func (decoder ClientRequestDecoder) DecodeClientRequest(doc carrier.Document) (wire.ClientDecodeResult, error) {
	var dto responsesRequestDTO
	if err := shared.DecodeExtensibleRequestObject(doc.RawBytes(), &dto, "responses request"); err != nil {
		return wire.ClientDecodeResult{}, err
	}
	exchangeID := strings.TrimSpace(doc.Meta.Opaque["exchange_id"]) // swobu:io-string source=boundary
	liteMarker := strings.EqualFold(strings.TrimSpace(doc.Header.Get(responsesLiteHeader)), "true") ||
		strings.EqualFold(strings.TrimSpace(dto.ClientMetadata[responsesLiteWebSocketMetadata]), "true")
	value, decisions, err := shared.WithAccumulatedDecisions(func(sink compat.Sink) (wire.ClientRequestResult, error) {
		access := mcp.Access{}
		request, delivery, err := decoder.decodeClientRequestDTOWithDecisions(dto, doc.RawBytes(), sink, exchangeID, responsesDecodeViewFull, liteMarker, &access)
		if err != nil {
			return wire.ClientRequestResult{}, err
		}
		explicit := strings.TrimSpace(dto.PreviousResponseWireID) != "" // swobu:io-string source=boundary
		history, err := fingerprintResponsesHistory(dto.Input, explicit, liteMarker)
		if err != nil {
			return wire.ClientRequestResult{}, err
		}
		result := wire.ClientRequestResult{Request: request, Delivery: delivery, MCPAccess: access, RequestFingerprint: history.request}
		if !explicit && history.previous != nil {
			rebasedDTO := dto
			rebasedDTO.Input = history.current
			raw, err := json.Marshal(rebasedDTO)
			if err != nil {
				return wire.ClientRequestResult{}, err
			}
			rebasedAccess := mcp.Access{}
			rebased, _, err := decoder.decodeClientRequestDTOWithDecisions(rebasedDTO, raw, nil, exchangeID, responsesDecodeViewRebasedCurrent, liteMarker, &rebasedAccess)
			if err != nil {
				return wire.ClientRequestResult{}, err
			}
			result.RebasedRequest = &wire.RebasedRequest{Previous: *history.previous, Request: rebased}
		}
		return result, nil
	})
	return wire.ClientDecodeResult{Request: value, Decisions: decisions}, err
}

// swobu:lint ignore function-complexity because=Responses request decoding validates all request bands at one protocol boundary.
func (decoder ClientRequestDecoder) decodeClientRequestWithDecisions(doc carrier.Document, sink compat.Sink, exchangeID string) (canonical.CanonicalRequest, delivery.Delivery, error) {
	raw := doc.RawBytes()
	var dto responsesRequestDTO
	if err := shared.DecodeExtensibleRequestObject(raw, &dto, "responses request"); err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	liteMarker := strings.EqualFold(strings.TrimSpace(doc.Header.Get(responsesLiteHeader)), "true") ||
		strings.EqualFold(strings.TrimSpace(dto.ClientMetadata[responsesLiteWebSocketMetadata]), "true")
	access := mcp.Access{}
	return decoder.decodeClientRequestDTOWithDecisions(dto, raw, sink, exchangeID, responsesDecodeViewFull, liteMarker, &access)
}

// swobu:lint ignore function-complexity because=Responses request decoding validates all request bands at one protocol boundary.
func (decoder ClientRequestDecoder) decodeClientRequestDTOWithDecisions(dto responsesRequestDTO, raw []byte, sink compat.Sink, exchangeID string, decodeView string, liteMarker bool, access *mcp.Access) (canonical.CanonicalRequest, delivery.Delivery, error) {
	if err := decodeResponsesWebSearchInclude(dto.Include, sink, exchangeID); err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	if err := decodeResponsesStore(dto.Store); err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	supplied, err := decodeResponsesSuppliedFields(raw)
	if err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	logResponsesRawInput(dto.Input, strings.TrimSpace(dto.PreviousResponseWireID), exchangeID, decodeView) // swobu:io-string source=boundary
	streamRequested, err := core.DecodeRequestStreamFlag(raw, "responses")
	if err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	if strings.TrimSpace(dto.PreviousResponseWireID) != "" && strings.TrimSpace(dto.Conversation) != "" { // swobu:io-string source=boundary
		if err := emitResponsesCompatibilityDecision(sink, exchangeID, compat.RequestPreviousResponse, compat.Reject, compat.Subject("wire:/previous_response_id")); err != nil {
			return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
		}
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), canonical.BadRequest("responses request must not specify both previous_response_id and conversation")
	}
	if strings.TrimSpace(dto.Conversation) != "" { // swobu:io-string source=boundary
		if err := emitResponsesCompatibilityDecision(sink, exchangeID, compat.WireConversation, compat.Reject, compat.Subject("wire:/conversation")); err != nil {
			return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
		}
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), canonical.BadRequest("responses conversation is not supported in swobu v0")
	}
	toolContextItems, tools, updatedAccess, err := decodeResponsesToolOccurrences(dto.Tools, canonical.ContextScopeRequest, "wire:/tools", sink, exchangeID, *access)
	if err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	*access = updatedAccess
	lite, err := classifyResponsesLite(dto, supplied, liteMarker)
	if err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	conversation, err := decodeResponsesInput(dto.Input, tools, lite, sink, exchangeID, decoder.ImageLimits, access)
	if err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	if err := shared.ValidateImageDecodeLimits(conversation, decoder.ImageLimits); err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), canonical.BadRequest("responses request image limits are exceeded")
	}
	logResponsesRequestImages(conversation, exchangeID, decodeView)
	if len(conversation) == 0 && !responsesNativeInputPresent(dto.Input) && strings.TrimSpace(dto.PreviousResponseWireID) == "" { // swobu:io-string source=boundary
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), canonical.BadRequest("responses request is missing required fields")
	}
	slog.Debug("responses request tools",
		"component", "httpapi",
		"event", "responses_request_tools",
		"exchange_id", exchangeID,
		"decode_view", decodeView,
		"tool_count", len(tools),
		"function_tool_count", responsesToolKindCount(tools, canonical.ToolTypeFunction),
		"custom_tool_count", responsesToolKindCount(tools, canonical.ToolTypeCustom),
		"web_search_tool_count", responsesToolKindCount(tools, canonical.ToolTypeWebSearch),
	)
	toolPolicy, err := DecodeResponsesToolPolicy(dto.ToolChoice, tools, sink, exchangeID)
	if err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	toolCallBatch, err := decodeResponsesToolCallBatch(dto.ParallelToolCalls)
	if err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	controls, err := decodeResponsesGenerationControls(dto)
	if err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	reasoning, err := decodeResponsesReasoning(dto.Reasoning, dto.Include, sink, exchangeID)
	if err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	outputFormat, err := decodeResponsesOutputFormat(dto.Text, sink, exchangeID)
	if err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	instructions, err := decodeResponsesInstructions(dto.Instructions)
	if err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	var previousResponse *canonical.ResponseRef
	clientPreviousSwobuResponseID := canonical.NewSwobuResponseID(dto.PreviousResponseWireID)
	if dto.PreviousResponseWireID != "" && clientPreviousSwobuResponseID.IsZero() { // swobu:io-string source=boundary
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), canonical.BadRequest("previous_response_id is empty")
	}
	if !clientPreviousSwobuResponseID.IsZero() {
		previousResponse = &canonical.ResponseRef{SwobuID: clientPreviousSwobuResponseID}
	}
	params := canonical.RequestParams{
		Items:            conversation,
		Controls:         controls,
		Reasoning:        reasoning,
		PreviousResponse: previousResponse,
	}
	if supplied.Model {
		params.Model = canonical.Specify(strings.TrimSpace(dto.Model)) // swobu:io-string source=boundary
	} // swobu:io-string source=boundary
	contextItems := make([]canonical.CanonicalItem, 0, len(conversation)+len(toolContextItems)+1)
	if supplied.Instructions && !lite {
		directive, err := canonical.NewScopedMessageItem(canonical.MessageRoleSystem, []canonical.MessagePart{canonical.NewTextMessagePart(instructions)}, canonical.ContextScopeRequest)
		if err != nil {
			return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
		}
		contextItems = append(contextItems, directive)
	}
	if supplied.Tools {
		contextItems = append(contextItems, toolContextItems...)
	}
	contextItems = append(contextItems, conversation...)
	if _, err := canonical.ToolEnvironmentAt(contextItems, len(contextItems)); err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), canonical.BadRequest("responses tool environment is ambiguous")
	}
	params.Items = contextItems
	if supplied.ToolPolicy {
		params.ToolPolicy = canonical.Specify(toolPolicy)
	}
	if supplied.ToolCallBatch {
		params.ToolCallBatch = canonical.Specify(toolCallBatch)
	}
	if supplied.OutputFormat {
		params.OutputFormat = canonical.Specify(outputFormat)
	}
	request := canonical.NewCanonicalRequest(params)
	resolvedDelivery := delivery.BufferedDelivery()
	if streamRequested {
		resolvedDelivery = delivery.StreamingDelivery(delivery.FramingNone)
	}
	return request, resolvedDelivery, nil
}

func classifyResponsesLite(dto responsesRequestDTO, supplied responsesSuppliedFields, marker bool) (bool, error) {
	if !marker {
		return false, nil
	}
	if supplied.Tools {
		return false, nil
	}
	instructions, err := decodeResponsesInstructions(dto.Instructions)
	if err != nil {
		return false, nil
	}
	if instructions != "" {
		return false, nil
	}
	var items []responsesInputItemDTO
	if err := json.Unmarshal(dto.Input, &items); err != nil || len(items) == 0 ||
		strings.TrimSpace(items[0].Type) != "additional_tools" ||
		strings.TrimSpace(items[0].Role) != "developer" {
		return false, nil
	}
	return true, nil
}

func responsesNativeInputPresent(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return false
	}
	if trimmed[0] != '[' {
		return true
	}
	var items []json.RawMessage
	return json.Unmarshal(trimmed, &items) == nil && len(items) > 0
}

func decodeResponsesStore(raw json.RawMessage) error {
	value := bytes.TrimSpace(raw)
	if len(value) == 0 || bytes.Equal(value, []byte("null")) {
		return nil
	}
	var storageIntent bool
	if err := json.Unmarshal(value, &storageIntent); err != nil {
		return canonical.BadRequest("responses store must be a boolean")
	}
	// `store` is a client wire hint, not authority over Swobu continuation
	// truth. The exchange always checkpoints successfully encoded responses so
	// opaque provider reasoning and resolved media survive client projection.
	return nil
}

func decodeResponsesInstructions(raw json.RawMessage) (string, error) {
	trimmed := strings.TrimSpace(string(raw)) // swobu:io-string source=boundary
	if trimmed == "" || trimmed == "null" {
		return "", nil
	}
	var instructions string
	if err := json.Unmarshal(raw, &instructions); err != nil {
		return "", canonical.BadRequest("responses request instructions is invalid")
	}
	return strings.TrimSpace(instructions), nil // swobu:io-string source=boundary
}

func logResponsesRawInput(input json.RawMessage, previousResponseID string, exchangeID string, decodeView string) {
	raw := strings.TrimSpace(string(input)) // swobu:io-string source=boundary
	shape := "null"
	if raw != "" && raw != "null" {
		switch raw[0] {
		case '[':
			shape = "array"
		case '{':
			shape = "object"
		case '"':
			shape = "string"
		default:
			shape = "other"
		}
	}
	slog.Debug("responses input summary",
		"component", "httpapi",
		"event", "responses_input_summary",
		"exchange_id", exchangeID,
		"decode_view", decodeView,
		"has_previous_response_id", previousResponseID != "",
		"input_shape", shape,
		"encoded_bytes", len(input),
	)
}

// logResponsesRequestImages records placement and correlation provenance only.
// Payload text, bytes, data URLs, source URLs, and tool arguments must never
// cross this diagnostic boundary.
func logResponsesRequestImages(items []canonical.CanonicalItem, exchangeID string, decodeView string) {
	toolCalls := make(map[canonical.ToolCallID]canonical.ToolKey)
	messageImageCount := 0
	toolResultImageCount := 0
	firstItem, firstPart := -1, -1
	firstCallID, firstToolName, firstToolKind, firstSource := "", "", "", ""

	for itemIndex, item := range items {
		if call, ok := item.ToolCall(); ok {
			toolCalls[call.CallID()] = call.Tool()
			continue
		}
		if message, ok := item.Message(); ok {
			for _, part := range message.Content() {
				if _, ok := part.Image(); ok {
					messageImageCount++
				}
			}
			continue
		}
		result, ok := item.ToolResult()
		if !ok {
			continue
		}
		for partIndex, part := range result.Content() {
			image, ok := part.Image()
			if !ok {
				continue
			}
			toolResultImageCount++
			if firstItem >= 0 {
				continue
			}
			firstItem, firstPart = itemIndex, partIndex
			firstCallID = result.CallID().String()
			firstSource = responsesImageSourceKind(image)
			if tool, exists := toolCalls[result.CallID()]; exists {
				firstToolName = tool.Name()
				firstToolKind = string(tool.Kind())
			}
		}
	}
	if messageImageCount+toolResultImageCount == 0 {
		return
	}
	slog.Debug("responses request images",
		"component", "httpapi",
		"event", "responses_request_images",
		"exchange_id", exchangeID,
		"decode_view", decodeView,
		"message_image_count", messageImageCount,
		"tool_result_image_count", toolResultImageCount,
		"first_tool_result_item", firstItem,
		"first_tool_result_part", firstPart,
		"first_tool_call_id", firstCallID,
		"first_tool_name", firstToolName,
		"first_tool_kind", firstToolKind,
		"first_source", firstSource,
	)
}

func responsesImageSourceKind(image canonical.ImagePart) string {
	if _, ok := image.Source().Inline(); ok {
		return "inline"
	}
	if _, ok := image.Source().URL(); ok {
		return "url"
	}
	return ""
}
