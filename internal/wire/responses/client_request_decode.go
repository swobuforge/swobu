// swobu:lint ignore file-length because=Responses request decoding keeps the protocol DTO-to-canonical boundary cohesive
package responses

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
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

func (decoder ClientRequestDecoder) DecodeClientRequest(doc carrier.Document) (wire.ClientDecodeResult, error) {
	var dto responsesRequestDTO
	if err := sse.DecodePermissiveJSON(doc.RawBytes(), &dto, "responses request", nil); err != nil {
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
		result := wire.ClientRequestResult{Request: request, Delivery: delivery, RequestFingerprint: history.request}
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
	if err := sse.DecodePermissiveJSON(raw, &dto, "responses request", nil); err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	return decoder.decodeClientRequestDTOWithDecisions(dto, raw, sink, exchangeID)
}

// swobu:lint ignore function-complexity because=Responses request decoding validates all request bands at one protocol boundary.
func (decoder ClientRequestDecoder) decodeClientRequestDTOWithDecisions(dto responsesRequestDTO, raw []byte, sink compat.Sink, exchangeID string) (canonical.CanonicalRequest, delivery.Delivery, error) {
	if len(bytes.TrimSpace(dto.Store)) != 0 {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), canonical.UnsupportedOperation("responses store is not supported")
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
	if len(conversation) == 0 && strings.TrimSpace(dto.PreviousResponseWireID) == "" { // swobu:io-string source=boundary
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), canonical.BadRequest("responses request is missing required fields")
	}
	slog.Debug("responses request tools",
		"component", "httpapi",
		"event", "responses_request_tools",
		"tool_count", len(tools),
		"function_tool_count", responsesToolKindCount(tools, canonical.ToolTypeFunction),
		"custom_tool_count", responsesToolKindCount(tools, canonical.ToolTypeCustom),
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

// swobu:lint ignore function-complexity because=responses input decoding keeps all acceptance branches in one protocol boundary helper.
func decodeResponsesInput(raw json.RawMessage, tools []canonical.ToolDeclaration, sink compat.Sink, exchangeID string, imageLimits shared.ImageDecodeLimitPolicy) ([]canonical.CanonicalItem, error) {
	raw = json.RawMessage(strings.TrimSpace(string(raw))) // swobu:io-string source=boundary
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		message, err := canonical.NewMessageItem(canonical.MessageRoleUser, []canonical.MessagePart{canonical.NewTextMessagePart(text)})
		if err != nil {
			return nil, canonical.BadRequest("responses request input text is invalid")
		}
		return []canonical.CanonicalItem{message}, nil
	}
	var items []responsesInputItemDTO
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, canonical.BadRequest("responses request input is invalid")
	}
	decoded := make([]canonical.CanonicalItem, 0, len(items))
	for idx, item := range items {
		itemType := strings.TrimSpace(item.Type) // swobu:io-string source=boundary
		if itemType == "" {
			if err := emitResponsesCompatibilityDecision(sink, exchangeID, compat.RequestItemsKind, compat.Approx, responsesInputSubject(idx, "type")); err != nil {
				return nil, err
			}
			itemType = "message"
		}
		switch itemType {
		case "message":
			role := strings.TrimSpace(item.Role) // swobu:io-string source=boundary
			if role == "" {
				if err := emitResponsesCompatibilityDecision(sink, exchangeID, compat.RequestItemsMessageRole, compat.Approx, responsesInputSubject(idx, "role")); err != nil {
					return nil, err
				}
				role = "user"
			}
			author := openaiwire.AuthorForRole(role)
			if role == "system" {
				author = canonical.MessageRoleSystem
			} else if role == "developer" {
				author = canonical.MessageRoleDeveloper
			}
			parts, err := decodeResponsesMessageContent(item.Content, author, imageLimits)
			if err != nil {
				return nil, err
			}
			decoded = append(decoded, parts...)
		case "function_call":
			callID := strings.TrimSpace(item.CallID) // swobu:io-string source=boundary
			if callID == "" {
				callID = strings.TrimSpace(item.ID) // swobu:io-string source=boundary
			}
			if callID == "" {
				if err := emitResponsesCompatibilityDecision(sink, exchangeID, compat.RequestItemsToolCallCallID, compat.Approx, responsesInputSubject(idx, "call_id")); err != nil {
					return nil, err
				}
				callID = openaiwire.GeneratedToolUseID(idx, 0)
			} else if strings.TrimSpace(item.CallID) == "" { // swobu:io-string source=boundary
				if err := emitResponsesCompatibilityDecision(sink, exchangeID, compat.RequestItemsToolCallCallID, compat.Approx, responsesInputSubject(idx, "call_id")); err != nil {
					return nil, err
				}
			}
			if strings.TrimSpace(item.Name) == "" { // swobu:io-string source=boundary
				return nil, canonical.BadRequest("responses request function_call items require a name")
			}
			input, err := decodeResponsesFunctionCallArguments(item.Arguments)
			if err != nil {
				return nil, err
			}
			toolKey, err := canonical.ResolveHistoricalToolKeyByName(tools, item.Name, canonical.ToolKindFunction)
			if err != nil {
				return nil, canonical.BadRequest("responses request function_call has an invalid tool identity")
			}
			canonicalCallID, _ := canonical.NewToolCallID(callID)
			call, err := canonical.NewToolCallItem(canonicalCallID, toolKey, canonical.NewJSONObjectToolInput(input))
			if err != nil {
				return nil, canonical.BadRequest("responses request function_call is invalid")
			}
			decoded = append(decoded, call)
		case "function_call_output":
			callID := strings.TrimSpace(item.CallID) // swobu:io-string source=boundary
			if callID == "" {
				if err := emitResponsesCompatibilityDecision(sink, exchangeID, compat.RequestItemsToolResultCallID, compat.Reject, responsesInputSubject(idx, "call_id")); err != nil {
					return nil, err
				}
				return nil, canonical.BadRequest("responses request function_call_output items require call_id")
			}
			output, err := decodeResponseOutputParts(item.Output, imageLimits)
			if err != nil {
				return nil, err
			}
			canonicalCallID, _ := canonical.NewToolCallID(callID)
			result, err := canonical.NewToolResultItem(canonicalCallID, output, false)
			if err != nil {
				return nil, canonical.BadRequest("responses request function_call_output is invalid")
			}
			decoded = append(decoded, result)
		default:
			if err := emitResponsesCompatibilityDecision(sink, exchangeID, compat.RequestItemsKind, compat.Reject, responsesInputSubject(idx, "type")); err != nil {
				return nil, err
			}
			return nil, canonical.BadRequest("responses request input contains an unsupported item type")
		}
	}
	return decoded, nil
}

// decodeResponsesMessageContent preserves the scalar-input acceptance surface:
// an explicit empty input_text is the appendable history form of input: "".
// Other OpenAI-family codecs retain their own empty-part validity rules.
func decodeResponsesMessageContent(raw json.RawMessage, author canonical.MessageRole, imageLimits shared.ImageDecodeLimitPolicy) ([]canonical.CanonicalItem, error) {
	parts, err := openaiwire.DecodeContentParts(raw, "responses message content is invalid")
	if err != nil {
		return nil, err
	}
	content := make([]canonical.MessagePart, 0, len(parts))
	for _, part := range parts {
		partType := strings.TrimSpace(part.Type) // swobu:io-string source=boundary
		if partType == "" {
			partType = "text"
		}
		switch partType {
		case "text", "input_text", "output_text":
			value := part.Text
			if value == "" {
				value = part.InputText
			}
			if value == "" {
				value = part.OutputText
			}
			content = append(content, canonical.NewTextMessagePart(value))
		case "image_url", "input_image":
			if author != canonical.MessageRoleUser {
				return nil, canonical.BadRequest("responses image input is only valid in user messages")
			}
			if strings.TrimSpace(part.FileID) != "" { // swobu:io-string source=provider-wire
				return nil, canonical.BadRequest("responses provider-scoped image file IDs are not portable")
			}
			image, err := openaiwire.DecodeOpenAIImage(part.ImageURL, "responses", imageLimits, part.Detail)
			if err != nil {
				return nil, err
			}
			content = append(content, canonical.NewImageMessagePart(image))
		default:
			return nil, canonical.BadRequest("responses message content contains an unsupported part type")
		}
	}
	if len(content) == 0 {
		return nil, nil
	}
	message, err := canonical.NewMessageItem(author, content)
	if err != nil {
		return nil, canonical.BadRequest("responses message author is invalid")
	}
	return []canonical.CanonicalItem{message}, nil
}

// OpenAI-family Responses bridges may stringify function_call.arguments when
// they rebuild a request item from a prior response. Accept either the raw
// object or the stringified object and normalize to one canonical JSON object
// at the request boundary.
func decodeResponsesFunctionCallArguments(raw json.RawMessage) (canonical.JSONObject, error) {
	input, err := canonical.ParseJSONObject(raw)
	if err == nil {
		return input, nil
	}
	var stringified string
	if err := json.Unmarshal(raw, &stringified); err != nil {
		return canonical.JSONObject{}, canonical.BadRequest("responses request function_call arguments are invalid")
	}
	trimmedStringified := strings.TrimSpace(stringified) // swobu:io-string source=boundary
	input, err = canonical.ParseJSONObject([]byte(trimmedStringified))
	if err != nil {
		return canonical.JSONObject{}, canonical.BadRequest("responses request function_call arguments are invalid")
	}
	return input, nil
}

func emitResponsesCompatibilityDecision(sink compat.Sink, exchangeID string, feature compat.Feature, outcome compat.Outcome, subject compat.Subject) error {
	if sink == nil {
		return nil
	}
	if subject == "" {
		return nil
	}
	if err := sink.Commit(context.Background(), exchangeID, []compat.Decision{
		compat.Decision{
			Feature: feature,
			Outcome: outcome,
			Subject: subject,
		},
	}); err != nil {
		return canonical.InternalError("compatibility decision sink commit failed")
	}
	return nil
}

func responsesInputSubject(index int, field string) compat.Subject {
	field = strings.TrimSpace(field) // swobu:io-string source=boundary
	if field == "" {
		return ""
	}
	return compat.Subject("wire:/input/" + strconv.Itoa(index) + "/" + field)
}

func decodeResponseOutputParts(raw json.RawMessage, imageLimits shared.ImageDecodeLimitPolicy) ([]canonical.ToolResultPart, error) {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return []canonical.ToolResultPart{canonical.NewTextToolResultPart(text)}, nil
	}
	var content []openaiwire.ContentPartItem
	if err := json.Unmarshal(raw, &content); err != nil {
		return nil, canonical.BadRequest("responses request function_call_output is invalid")
	}
	parts := make([]canonical.ToolResultPart, 0, len(content))
	for _, part := range content {
		partType := strings.TrimSpace(part.Type) // swobu:io-string source=boundary
		switch partType {
		case "", "text", "input_text", "output_text":
			value := part.Text
			if value == "" {
				value = part.InputText
			}
			if value == "" {
				value = part.OutputText
			}
			parts = append(parts, canonical.NewTextToolResultPart(value))
		case "input_image", "image_url":
			if strings.TrimSpace(part.FileID) != "" { // swobu:io-string source=provider-wire
				return nil, canonical.BadRequest("responses request function_call_output provider-scoped image file IDs are not portable")
			}
			image, err := openaiwire.DecodeOpenAIImage(part.ImageURL, "responses function_call_output", imageLimits, part.Detail)
			if err != nil {
				return nil, err
			}
			parts = append(parts, canonical.NewImageToolResultPart(image))
		case "input_file", "file":
			return nil, canonical.BadRequest("responses request function_call_output file content is not portable")
		default:
			return nil, canonical.BadRequest("responses request function_call_output contains an unsupported part type")
		}
	}
	return parts, nil
}
