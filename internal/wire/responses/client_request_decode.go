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
	"github.com/swobuforge/swobu/internal/domain/responsesnative"
	"github.com/swobuforge/swobu/internal/wire"
	core "github.com/swobuforge/swobu/internal/wire/primitives"
	shared "github.com/swobuforge/swobu/internal/wire/shared"
)

func (decoder ClientRequestDecoder) DecodeClientRequest(doc carrier.Document) (wire.ClientDecodeResult, error) {
	var dto responsesRequestDTO
	if err := shared.DecodeExtensibleRequestObject(doc.RawBytes(), &dto, "responses request"); err != nil {
		return wire.ClientDecodeResult{}, err
	}
	value, decisions, err := shared.WithAccumulatedDecisions(func(sink compat.Sink) (wire.ClientRequestResult, error) {
		request, delivery, err := decoder.decodeClientRequestDTOWithDecisions(dto, doc.RawBytes(), sink, "")
		if err != nil {
			return wire.ClientRequestResult{}, err
		}
		explicit := strings.TrimSpace(dto.PreviousResponseWireID) != "" // swobu:io-string source=boundary
		history, err := fingerprintResponsesHistory(dto.Input, explicit)
		if err != nil {
			return wire.ClientRequestResult{}, err
		}
		nativeInput, err := captureResponsesInputItems(dto.Input)
		if err != nil {
			return wire.ClientRequestResult{}, err
		}
		result := wire.ClientRequestResult{Request: request, Delivery: delivery, ResponsesInput: nativeInput, RequestFingerprint: history.request}
		if !explicit && history.previous != nil {
			rebasedDTO := dto
			rebasedDTO.Input = history.current
			raw, err := json.Marshal(rebasedDTO)
			if err != nil {
				return wire.ClientRequestResult{}, err
			}
			rebased, _, err := decoder.decodeClientRequestDTOWithDecisions(rebasedDTO, raw, nil, "")
			if err != nil {
				return wire.ClientRequestResult{}, err
			}
			rebasedNative, err := captureResponsesInputItems(history.current)
			if err != nil {
				return wire.ClientRequestResult{}, err
			}
			result.RebasedRequest = &wire.RebasedRequest{Previous: *history.previous, Request: rebased, ResponsesInput: rebasedNative}
		}
		return result, nil
	})
	return wire.ClientDecodeResult{Request: value, Decisions: decisions}, err
}

func captureResponsesInputItems(raw json.RawMessage) (responsesnative.Items, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) || trimmed[0] != '[' {
		return responsesnative.Items{}, nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(trimmed, &items); err != nil {
		return responsesnative.Items{}, canonical.BadRequest("responses request input is invalid")
	}
	values := make([][]byte, len(items))
	for index := range items {
		value, err := replayableResponsesInputItem(items[index])
		if err != nil {
			return responsesnative.Items{}, canonical.BadRequest("responses request input contains an invalid native item")
		}
		values[index] = value
	}
	preserved, err := responsesnative.NewItems(values)
	if err != nil {
		return responsesnative.Items{}, canonical.BadRequest("responses request input contains an invalid native item")
	}
	return preserved, nil
}

var responsesProviderItemIDPrefixes = map[string]string{
	"function_call":   "fc",
	"message":         "msg",
	"reasoning":       "rs",
	"web_search_call": "ws",
}

// replayableResponsesInputItem owns the distinction between client-local item
// identity and provider continuation identity. Some Responses clients assign
// UI-oriented IDs such as "item_0" to known input items. Those values are valid
// client metadata but invalid when replayed into provider-owned ID namespaces.
// Unknown item kinds remain opaque so future Responses fields survive intact.
func replayableResponsesInputItem(raw json.RawMessage) ([]byte, error) {
	var identity struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	}
	if err := json.Unmarshal(raw, &identity); err != nil {
		return nil, err
	}
	prefix, providerOwned := responsesProviderItemIDPrefixes[identity.Type]
	if !providerOwned || identity.ID == "" || strings.HasPrefix(identity.ID, prefix) {
		return raw, nil
	}

	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, err
	}
	delete(object, "id")
	return json.Marshal(object)
}

// swobu:lint ignore function-complexity because=Responses request decoding validates all request bands at one protocol boundary.
func (decoder ClientRequestDecoder) decodeClientRequestWithDecisions(doc carrier.Document, sink compat.Sink, exchangeID string) (canonical.CanonicalRequest, delivery.Delivery, error) {
	raw := doc.RawBytes()
	var dto responsesRequestDTO
	if err := shared.DecodeExtensibleRequestObject(raw, &dto, "responses request"); err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	return decoder.decodeClientRequestDTOWithDecisions(dto, raw, sink, exchangeID)
}

// swobu:lint ignore function-complexity because=Responses request decoding validates all request bands at one protocol boundary.
func (decoder ClientRequestDecoder) decodeClientRequestDTOWithDecisions(dto responsesRequestDTO, raw []byte, sink compat.Sink, exchangeID string) (canonical.CanonicalRequest, delivery.Delivery, error) {
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
	logResponsesRawInput(dto.Input, strings.TrimSpace(dto.PreviousResponseWireID)) // swobu:io-string source=boundary
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
	tools, err := decodeResponsesTools(dto.Tools, sink, exchangeID)
	if err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	toolSet, err := canonical.NewToolSet(tools)
	if err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), canonical.BadRequest("responses tools are invalid")
	}
	conversation, err := decodeResponsesInput(dto.Input, tools, sink, exchangeID, decoder.ImageLimits)
	if err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	if err := shared.ValidateImageDecodeLimits(conversation, decoder.ImageLimits); err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), canonical.BadRequest("responses request image limits are exceeded")
	}
	if len(conversation) == 0 && !responsesNativeInputPresent(dto.Input) && strings.TrimSpace(dto.PreviousResponseWireID) == "" { // swobu:io-string source=boundary
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), canonical.BadRequest("responses request is missing required fields")
	}
	slog.Debug("responses request tools",
		"component", "httpapi",
		"event", "responses_request_tools",
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
	reasoning, err := decodeResponsesReasoning(dto.Reasoning, dto.Include)
	if err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	outputFormat, err := decodeResponsesOutputFormat(dto.Text)
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
	if supplied.Instructions {
		params.Instructions = canonical.Specify(canonical.NewSystemInstructionSet(instructions))
	}
	if supplied.Tools {
		params.Tools = canonical.Specify(toolSet)
	}
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

func logResponsesRawInput(input json.RawMessage, previousResponseID string) {
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
		"has_previous_response_id", previousResponseID != "",
		"input_shape", shape,
		"encoded_bytes", len(input),
	)
}
