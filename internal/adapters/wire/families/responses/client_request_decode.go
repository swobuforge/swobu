package responses

import (
	"bytes"
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
	var dto responsesRequestDTO
	if err := sse.DecodePermissiveJSON(raw, &dto, "responses request", nil); err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	logResponsesRawInput(dto.Input, strings.TrimSpace(dto.PreviousResponseID)) // swobu:io-string source=boundary
	streamRequested, err := core.DecodeRequestStreamFlag(raw, "responses")
	if err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	inputText, conversation, err := decodeResponsesInput(dto.Input)
	if err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	if strings.TrimSpace(dto.PreviousResponseID) != "" && strings.TrimSpace(dto.Conversation) != "" { // swobu:io-string source=boundary
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), canonical.BadRequest("responses request must not specify both previous_response_id and conversation")
	}
	if strings.TrimSpace(dto.Conversation) != "" { // swobu:io-string source=boundary
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), canonical.BadRequest("responses conversation is not supported in swobu v0")
	}
	if inputText == "" && len(conversation) == 0 && strings.TrimSpace(dto.PreviousResponseID) == "" { // swobu:io-string source=boundary
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), canonical.BadRequest("responses request is missing required fields")
	}
	tools, err := decodeResponsesTools(dto.Tools)
	if err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	toolPolicy, err := DecodeResponsesToolPolicy(dto.ToolChoice, tools)
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
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:         strings.TrimSpace(dto.Model), // swobu:io-string source=boundary
		InputText:     inputText,
		Items:         conversation,
		Tools:         tools,
		ToolPolicy:    toolPolicy,
		ToolCallBatch: toolCallBatch,
		Controls:      controls,
		OutputFormat:  outputFormat,
		Turn:          canonical.NewTurnRef(dto.PreviousResponseID), // swobu:io-string source=boundary
	})
	resolvedDelivery := delivery.BufferedDelivery()
	if streamRequested {
		resolvedDelivery = delivery.StreamingDelivery(delivery.FramingNone)
	}
	return request, resolvedDelivery, nil
}

func logResponsesRawInput(input json.RawMessage, previousResponseID string) {
	raw := strings.TrimSpace(string(input)) // swobu:io-string source=boundary
	if raw == "" {
		raw = "null"
	}
	normalized := raw
	var compact bytes.Buffer
	if err := json.Compact(&compact, []byte(raw)); err == nil {
		normalized = compact.String()
	}
	slog.Debug("responses raw input",
		"component", "httpapi",
		"event", "responses_raw_input",
		"has_previous_response_id", previousResponseID != "",
		"raw_input_json", normalized,
	)
}

func decodeResponsesInput(raw json.RawMessage) (string, []canonical.CanonicalItem, error) {
	raw = json.RawMessage(strings.TrimSpace(string(raw))) // swobu:io-string source=boundary
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil, nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text, nil, nil
	}
	var items []responsesInputItemDTO
	if err := json.Unmarshal(raw, &items); err != nil {
		return "", nil, canonical.BadRequest("responses request input is invalid")
	}
	decoded := make([]canonical.CanonicalItem, 0, len(items))
	for idx, item := range items {
		itemType := strings.TrimSpace(item.Type) // swobu:io-string source=boundary
		if itemType == "" {
			itemType = "message"
		}
		switch itemType {
		case "message":
			role := strings.TrimSpace(item.Role) // swobu:io-string source=boundary
			if role == "" {
				role = "user"
			}
			parts, err := openaicompat.DecodeTextContentItems(item.Content, "responses", openaicompat.AuthorForRole(role))
			if err != nil {
				return "", nil, err
			}
			decoded = append(decoded, parts...)
		case "function_call":
			callID := strings.TrimSpace(item.CallID) // swobu:io-string source=boundary
			if callID == "" {
				callID = strings.TrimSpace(item.ID) // swobu:io-string source=boundary
			}
			if callID == "" {
				callID = openaicompat.GeneratedToolUseID(idx, 0)
			}
			if strings.TrimSpace(item.Name) == "" { // swobu:io-string source=boundary
				return "", nil, canonical.BadRequest("responses request function_call items require a name")
			}
			input, err := sse.DecodeJSONObject(item.Arguments, "responses request function_call arguments are invalid")
			if err != nil {
				return "", nil, err
			}
			args, err := json.Marshal(input)
			if err != nil {
				return "", nil, canonical.BadRequest("responses request function_call arguments are invalid")
			}
			decoded = append(decoded, canonical.NewToolUseItem(canonical.ItemAuthorAssistant, "", callID, strings.TrimSpace(item.Name), canonical.NewToolArgumentsObject(string(args)))) // swobu:io-string source=boundary
		case "function_call_output":
			callID := strings.TrimSpace(item.CallID) // swobu:io-string source=boundary
			if callID == "" {
				return "", nil, canonical.BadRequest("responses request function_call_output items require call_id")
			}
			output, err := decodeResponseOutputText(item.Output)
			if err != nil {
				return "", nil, err
			}
			decoded = append(decoded, canonical.NewToolResultItem(canonical.ItemAuthorTool, callID, output))
		default:
			return "", nil, canonical.BadRequest("responses request input contains an unsupported item type")
		}
	}
	return "", decoded, nil
}

func decodeResponseOutputText(raw json.RawMessage) (string, error) {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text, nil
	}
	var content []responsesOutputTextPartDTO
	if err := json.Unmarshal(raw, &content); err != nil {
		return "", canonical.BadRequest("responses request function_call_output is invalid")
	}
	var builder strings.Builder
	for _, part := range content {
		partType := strings.TrimSpace(part.Type) // swobu:io-string source=boundary
		if partType != "" && partType != "text" && partType != "output_text" {
			return "", canonical.BadRequest("responses request function_call_output must contain text only")
		}
		builder.WriteString(part.Text)
	}
	return builder.String(), nil
}
