// translation in one place so request and stream semantics stay recoverable.
package responses

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"

	httpcodec "github.com/swobuforge/swobu/internal/adapters/outbound/protocols/httpcodec"
	openaicompat "github.com/swobuforge/swobu/internal/adapters/outbound/protocols/openaicompat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

type ResponsesCodec struct{}

func (ResponsesCodec) DecodeRequest(raw []byte) (canonical.CanonicalRequest, bool, error) {
	var dto responsesRequestDTO
	if err := json.Unmarshal(raw, &dto); err != nil {
		return nil, false, canonical.BadRequest("responses request body is invalid JSON")
	}
	logResponsesRawInput(dto.Input, strings.TrimSpace(dto.PreviousResponseID)) // trimlowerlint:allow boundary canonicalization
	inputText, conversation, err := decodeResponsesInput(dto.Input)
	if err != nil {
		return nil, false, err
	}
	if strings.TrimSpace(dto.PreviousResponseID) != "" && strings.TrimSpace(dto.Conversation) != "" { // trimlowerlint:allow boundary canonicalization
		return nil, false, canonical.BadRequest("responses request must not specify both previous_response_id and conversation")
	}
	if strings.TrimSpace(dto.Conversation) != "" { // trimlowerlint:allow boundary canonicalization
		return nil, false, canonical.BadRequest("responses conversation is not supported in swobu v0")
	}
	if inputText == "" && len(conversation) == 0 && strings.TrimSpace(dto.PreviousResponseID) == "" { // trimlowerlint:allow boundary canonicalization
		return nil, false, canonical.BadRequest("responses request is missing required fields")
	}
	toolMode, err := DecodeResponsesToolMode(dto.ToolChoice)
	if err != nil {
		return nil, false, err
	}
	request := canonical.NewGenerationRequest(canonical.GenerationRequestParams{
		Model:                strings.TrimSpace(dto.Model), // trimlowerlint:allow boundary canonicalization
		InputText:            inputText,
		Items:                conversation,
		ToolMode:             toolMode,
		PreviousResponseID:   strings.TrimSpace(dto.PreviousResponseID),   // trimlowerlint:allow boundary canonicalization
		PromptCacheKey:       strings.TrimSpace(dto.PromptCacheKey),       // trimlowerlint:allow boundary canonicalization
		PromptCacheRetention: strings.TrimSpace(dto.PromptCacheRetention), // trimlowerlint:allow boundary canonicalization
	})
	if err := canonical.ValidateResponseContinuationSelectors(request); err != nil {
		return nil, false, err
	}
	return request, dto.Stream, nil
}

func logResponsesRawInput(input json.RawMessage, previousResponseID string) {
	raw := strings.TrimSpace(string(input)) // trimlowerlint:allow boundary canonicalization
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

// DecodeResponsesToolMode is intentionally permissive for unknown enum/object
// values. Known values map to canonical tool modes, and unknown values degrade
// to default mode for forward canonical. Invalid JSON type shapes still fail
// request validation.
func DecodeResponsesToolMode(raw json.RawMessage) (canonical.ToolMode, error) {
	raw = json.RawMessage(strings.TrimSpace(string(raw))) // trimlowerlint:allow boundary canonicalization
	if len(raw) == 0 || string(raw) == "null" {
		return canonical.ToolModeDefault, nil
	}

	var stringMode string
	if err := json.Unmarshal(raw, &stringMode); err == nil {
		switch strings.ToLower(strings.TrimSpace(stringMode)) { // trimlowerlint:allow boundary canonicalization
		case "", "none":
			return canonical.ToolModeDefault, nil
		case "auto":
			return canonical.ToolModeAuto, nil
		case "required":
			return canonical.ToolModeRequired, nil
		default:
			return canonical.ToolModeDefault, nil
		}
	}

	var objectMode struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &objectMode); err != nil {
		return canonical.ToolModeDefault, canonical.BadRequest("responses request tool_choice is invalid")
	}
	switch strings.ToLower(strings.TrimSpace(objectMode.Type)) { // trimlowerlint:allow boundary canonicalization
	case "function", "required":
		return canonical.ToolModeRequired, nil
	case "auto":
		return canonical.ToolModeAuto, nil
	default:
		return canonical.ToolModeDefault, nil
	}
}

func (ResponsesCodec) EncodeBuffered(output canonical.CanonicalOutput) ([]byte, error) {
	encoded := make([]responsesOutputItemDTO, 0, len(output.Items()))
	outputText := ""
	for _, item := range output.Items() {
		switch item.Kind {
		case canonical.ItemKindText:
			text := item.Text
			outputText += text
			encoded = append(encoded, responsesOutputItemDTO{
				Type:   "message",
				Status: "completed",
				Role:   "assistant",
				Content: []responsesOutputTextItemDTO{{
					Type: "output_text",
					Text: text,
				}},
			})
		case canonical.ItemKindToolUse:
			args, _ := json.Marshal(item.Input)
			encoded = append(encoded, responsesOutputItemDTO{
				Type:      "function_call",
				CallID:    item.ToolUseID,
				Name:      item.Name,
				Arguments: string(args),
			})
		}
	}
	encodedBody, err := json.Marshal(responsesResponseDTO{
		ID:         httpcodec.FallbackID(output.ResultID(), "resp_swobu"),
		Object:     "response",
		Model:      output.Model(),
		Status:     "completed",
		OutputText: outputText,
		Output:     encoded,
		Usage:      responsesUsageFromCanonical(output.Usage()),
	})
	if err != nil {
		return nil, err
	}
	logResponsesEgressBuffered(encodedBody)
	return encodedBody, nil
}

func (ResponsesCodec) NewStreamState() httpcodec.EnvelopeStreamEncoder {
	return &responsesClientStreamEncoder{adapter: httpcodec.NewEnvelopeEventAdapter()}
}

// family's polymorphic item shapes at the transport edge.
func decodeResponsesInput(raw json.RawMessage) (string, []canonical.CanonicalItem, error) {
	raw = json.RawMessage(strings.TrimSpace(string(raw))) // trimlowerlint:allow boundary canonicalization
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
		itemType := strings.TrimSpace(item.Type) // trimlowerlint:allow boundary canonicalization
		if itemType == "" {
			itemType = "message"
		}
		switch itemType {
		case "message":
			role := strings.TrimSpace(item.Role) // trimlowerlint:allow boundary canonicalization
			if role == "" {
				role = "user"
			}
			parts, err := openaicompat.DecodeTextContentItems(item.Content, "responses", openaicompat.AuthorForRole(role))
			if err != nil {
				return "", nil, err
			}
			decoded = append(decoded, parts...)
		case "function_call":
			callID := strings.TrimSpace(item.CallID) // trimlowerlint:allow boundary canonicalization
			if callID == "" {
				callID = strings.TrimSpace(item.ID) // trimlowerlint:allow boundary canonicalization
			}
			if callID == "" {
				callID = openaicompat.GeneratedToolUseID(idx, 0)
			}
			if strings.TrimSpace(item.Name) == "" { // trimlowerlint:allow boundary canonicalization
				return "", nil, canonical.BadRequest("responses request function_call items require a name")
			}
			input, err := httpcodec.DecodeJSONObject(item.Arguments, "responses request function_call arguments are invalid")
			if err != nil {
				return "", nil, err
			}
			decoded = append(decoded, canonical.NewToolUseItem(canonical.ItemAuthorAssistant, "", callID, strings.TrimSpace(item.Name), input)) // trimlowerlint:allow boundary canonicalization
		case "function_call_output":
			callID := strings.TrimSpace(item.CallID) // trimlowerlint:allow boundary canonicalization
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
		partType := strings.TrimSpace(part.Type) // trimlowerlint:allow boundary canonicalization
		if partType != "" && partType != "text" && partType != "output_text" {
			return "", canonical.BadRequest("responses request function_call_output must contain text only")
		}
		builder.WriteString(part.Text)
	}
	return builder.String(), nil
}

func responsesUsageFromCanonical(usage canonical.TokenUsage) *responsesUsageDTO {
	input, hasInput := usage.InputTokens()
	output, hasOutput := usage.OutputTokens()
	cacheRead, hasCacheRead := usage.CacheReadTokens()
	cacheWrite, hasCacheWrite := usage.CacheWriteTokens()
	if !hasInput && !hasOutput && !hasCacheRead && !hasCacheWrite {
		return nil
	}
	dto := &responsesUsageDTO{
		InputTokens:  input,
		OutputTokens: output,
		TotalTokens:  input + output,
	}
	if hasCacheRead || hasCacheWrite {
		dto.InputDetails = &responsesInputDetailsDTO{
			CachedTokens:     cacheRead,
			CacheWriteTokens: cacheWrite,
		}
	}
	return dto
}

type responsesClientStreamEncoder struct {
	wire    ResponsesClientStreamEncoderWire
	adapter *httpcodec.EnvelopeEventAdapter
}

func (s *responsesClientStreamEncoder) EncodeEnvelopeEvent(event canonical.Event) ([][]byte, error) {
	streamEvents := s.adapter.Translate(event)
	frames := make([][]byte, 0, len(streamEvents))
	for _, streamEvent := range streamEvents {
		emitted, err := s.Encode(streamEvent)
		if err != nil {
			return nil, err
		}
		frames = append(frames, emitted...)
	}
	return frames, nil
}

func (s *responsesClientStreamEncoder) Encode(event httpcodec.StreamEvent) ([][]byte, error) {
	encoder := s.encoder()
	rawFrames, err := encoder.Encode(event)
	if err != nil {
		return nil, err
	}
	for _, raw := range rawFrames {
		logResponsesEgressStreamFrame(raw)
	}
	frames := make([][]byte, 0, len(rawFrames))
	for _, raw := range rawFrames {
		frames = append(frames, httpcodec.SSEData(raw))
	}
	return frames, nil
}

func (s *responsesClientStreamEncoder) Finish() ([][]byte, error) {
	encoder := s.encoder()
	rawFrames, err := encoder.Finish()
	if err != nil {
		return nil, err
	}
	for _, raw := range rawFrames {
		logResponsesEgressStreamFrame(raw)
	}
	frames := make([][]byte, 0, len(rawFrames))
	for _, raw := range rawFrames {
		frames = append(frames, httpcodec.SSEData(raw))
	}
	return frames, nil
}

func (s *responsesClientStreamEncoder) encoder() *ResponsesClientStreamEncoderWire {
	if s.wire.toolItems == nil {
		s.wire = NewResponsesClientStreamEncoderWire()
	}
	return &s.wire
}
