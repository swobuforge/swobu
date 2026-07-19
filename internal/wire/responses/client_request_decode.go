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

func (ClientRequestDecoder) DecodeClientRequest(doc carrier.Document) (wire.ClientDecodeResult, error) {
	value, decisions, err := shared.WithAccumulatedDecisions(func(sink compat.Sink) (wire.ClientRequestResult, error) {
		request, delivery, err := (ClientRequestDecoder{}).decodeClientRequestWithDecisions(doc, sink, "")
		return wire.ClientRequestResult{
			Request:  request,
			Delivery: delivery,
		}, err
	})
	return wire.ClientDecodeResult{Request: value, Decisions: decisions}, err
}

func (ClientRequestDecoder) decodeClientRequestWithDecisions(doc carrier.Document, sink compat.Sink, exchangeID string) (canonical.CanonicalRequest, delivery.Delivery, error) {
	raw := doc.RawBytes()
	var dto responsesRequestDTO
	if err := sse.DecodePermissiveJSON(raw, &dto, "responses request", nil); err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	presence, err := decodeResponsesRequestPresence(raw)
	if err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	logResponsesRawInput(dto.Input, strings.TrimSpace(dto.PreviousResponseID)) // swobu:io-string source=boundary
	streamRequested, err := core.DecodeRequestStreamFlag(raw, "responses")
	if err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	inputText, conversation, err := decodeResponsesInput(dto.Input, sink, exchangeID)
	if err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	if strings.TrimSpace(dto.PreviousResponseID) != "" && strings.TrimSpace(dto.Conversation) != "" { // swobu:io-string source=boundary
		if err := emitResponsesCompatibilityDecision(sink, exchangeID, compat.RequestContinuation, compat.Reject, compat.Subject("wire:/previous_response_id")); err != nil {
			return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
		}
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), canonical.BadRequest("responses request must not specify both previous_response_id and conversation")
	}
	if strings.TrimSpace(dto.Conversation) != "" { // swobu:io-string source=boundary
		if err := emitResponsesCompatibilityDecision(sink, exchangeID, compat.RequestConversation, compat.Reject, compat.Subject("wire:/conversation")); err != nil {
			return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
		}
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), canonical.BadRequest("responses conversation is not supported in swobu v0")
	}
	if inputText == "" && len(conversation) == 0 && strings.TrimSpace(dto.PreviousResponseID) == "" { // swobu:io-string source=boundary
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), canonical.BadRequest("responses request is missing required fields")
	}
	tools, err := decodeResponsesTools(dto.Tools, sink, exchangeID)
	if err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	slog.Debug("responses request tools",
		"component", "httpapi",
		"event", "responses_request_tools",
		"tool_count", len(tools),
		"function_tool_count", responsesToolKindCount(tools, canonical.ToolTypeFunction),
		"custom_tool_count", responsesToolKindCount(tools, canonical.ToolTypeCustom),
		"capability_tool_count", responsesCapabilityToolCount(tools),
		"capability_tool_names", strings.Join(responsesCapabilityToolNames(tools), ","),
		"capability_tool_configs", strings.Join(responsesCapabilityToolConfigs(tools), " | "),
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
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:         strings.TrimSpace(dto.Model), // swobu:io-string source=boundary
		Instructions:  instructions,
		InputText:     inputText,
		Items:         conversation,
		Tools:         tools,
		ToolPolicy:    toolPolicy,
		ToolCallBatch: toolCallBatch,
		Controls:      controls,
		OutputFormat:  outputFormat,
		Turn:          canonical.NewTurnRef(dto.PreviousResponseID), // swobu:io-string source=boundary
		Presence:      presence,
	})
	resolvedDelivery := delivery.BufferedDelivery()
	if streamRequested {
		resolvedDelivery = delivery.StreamingDelivery(delivery.FramingNone)
	}
	return request, resolvedDelivery, nil
}

func decodeResponsesRequestPresence(raw []byte) (canonical.RequestPresence, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return canonical.RequestPresence{}, canonical.BadRequest("responses request is invalid")
	}
	_, model := fields["model"]
	_, instructions := fields["instructions"]
	_, tools := fields["tools"]
	_, toolPolicy := fields["tool_choice"]
	_, toolCallBatch := fields["parallel_tool_calls"]
	_, outputFormat := fields["text"]
	_, maxOutputTokens := fields["max_output_tokens"]
	_, stopSequences := fields["stop"]
	_, temperature := fields["temperature"]
	_, topP := fields["top_p"]
	return canonical.RequestPresence{
		Model:         model,
		Instructions:  instructions,
		Tools:         tools,
		ToolPolicy:    toolPolicy,
		ToolCallBatch: toolCallBatch,
		OutputFormat:  outputFormat,
		Controls: canonical.GenerationControlsPresence{
			MaxOutputTokens: maxOutputTokens,
			StopSequences:   stopSequences,
			Temperature:     temperature,
			TopP:            topP,
		},
	}, nil
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

// swobu:lint ignore function-complexity because=responses input decoding keeps all acceptance branches in one protocol boundary helper.
func decodeResponsesInput(raw json.RawMessage, sink compat.Sink, exchangeID string) (string, []canonical.CanonicalItem, error) {
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
			if err := emitResponsesCompatibilityDecision(sink, exchangeID, compat.RequestInputShape, compat.Approx, responsesInputSubject(idx, "type")); err != nil {
				return "", nil, err
			}
			itemType = "message"
		}
		switch itemType {
		case "message":
			role := strings.TrimSpace(item.Role) // swobu:io-string source=boundary
			if role == "" {
				if err := emitResponsesCompatibilityDecision(sink, exchangeID, compat.RequestRole, compat.Approx, responsesInputSubject(idx, "role")); err != nil {
					return "", nil, err
				}
				role = "user"
			}
			parts, err := openaiwire.DecodeTextContentItems(item.Content, "responses", openaiwire.AuthorForRole(role))
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
				if err := emitResponsesCompatibilityDecision(sink, exchangeID, compat.ToolCallID, compat.Approx, responsesInputSubject(idx, "call_id")); err != nil {
					return "", nil, err
				}
				callID = openaiwire.GeneratedToolUseID(idx, 0)
			} else if strings.TrimSpace(item.CallID) == "" { // swobu:io-string source=boundary
				if err := emitResponsesCompatibilityDecision(sink, exchangeID, compat.ToolCallID, compat.Approx, responsesInputSubject(idx, "call_id")); err != nil {
					return "", nil, err
				}
			}
			if strings.TrimSpace(item.Name) == "" { // swobu:io-string source=boundary
				return "", nil, canonical.BadRequest("responses request function_call items require a name")
			}
			input, err := decodeResponsesFunctionCallArguments(item.Arguments)
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
				if err := emitResponsesCompatibilityDecision(sink, exchangeID, compat.ToolResultID, compat.Reject, responsesInputSubject(idx, "call_id")); err != nil {
					return "", nil, err
				}
				return "", nil, canonical.BadRequest("responses request function_call_output items require call_id")
			}
			output, err := decodeResponseOutputText(item.Output)
			if err != nil {
				return "", nil, err
			}
			decoded = append(decoded, canonical.NewToolResultItem(canonical.ItemAuthorTool, callID, output))
		default:
			if err := emitResponsesCompatibilityDecision(sink, exchangeID, compat.RequestInputShape, compat.Reject, responsesInputSubject(idx, "type")); err != nil {
				return "", nil, err
			}
			return "", nil, canonical.BadRequest("responses request input contains an unsupported item type")
		}
	}
	return "", decoded, nil
}

func responsesCapabilityToolNames(tools []canonical.ToolDecl) []string {
	if len(tools) == 0 {
		return nil
	}
	out := make([]string, 0, len(tools))
	for _, tool := range tools {
		switch decl := tool.(type) {
		case canonical.CapabilityToolDecl:
			out = append(out, strings.TrimSpace(decl.ToolName()))
		case *canonical.CapabilityToolDecl:
			if decl != nil {
				out = append(out, strings.TrimSpace(decl.ToolName()))
			}
		}
	}
	return out
}

func responsesCapabilityToolConfigs(tools []canonical.ToolDecl) []string {
	if len(tools) == 0 {
		return nil
	}
	out := make([]string, 0, len(tools))
	for _, tool := range tools {
		switch decl := tool.(type) {
		case canonical.CapabilityToolDecl:
			out = append(out, strings.TrimSpace(decl.CapabilityConfig().RawObject()))
		case *canonical.CapabilityToolDecl:
			if decl != nil {
				out = append(out, strings.TrimSpace(decl.CapabilityConfig().RawObject()))
			}
		}
	}
	return out
}

// OpenAI-family Responses bridges may stringify function_call.arguments when
// they rebuild a request item from a prior response. Accept either the raw
// object or the stringified object and normalize to one canonical JSON object
// at the request boundary.
func decodeResponsesFunctionCallArguments(raw json.RawMessage) (map[string]any, error) {
	input, err := sse.DecodeJSONObject(raw, "responses request function_call arguments are invalid")
	if err == nil {
		return input, nil
	}
	var stringified string
	if err := json.Unmarshal(raw, &stringified); err != nil {
		return nil, canonical.BadRequest("responses request function_call arguments are invalid")
	}
	trimmedStringified := strings.TrimSpace(stringified) // swobu:io-string source=boundary
	return sse.DecodeJSONObject(json.RawMessage(trimmedStringified), "responses request function_call arguments are invalid")
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
