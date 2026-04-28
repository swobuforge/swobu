// translation in one place so request and stream semantics stay recoverable.
package httpapi

import (
	"encoding/json"
	"strings"

	"github.com/metrofun/swobu/internal/domain/compatibility"
)

type responsesFamilyCodec struct{}

func (responsesFamilyCodec) decodeRequest(raw []byte) (compatibility.CanonicalRequest, compatibility.DeliveryMode, error) {
	var dto responsesRequestDTO
	if err := json.Unmarshal(raw, &dto); err != nil {
		return nil, "", compatibility.BadRequest("responses request body is invalid JSON")
	}
	inputText, conversation, err := decodeResponsesInput(dto.Input)
	if err != nil {
		return nil, "", err
	}
	if strings.TrimSpace(dto.PreviousResponseID) != "" && strings.TrimSpace(dto.Conversation) != "" {
		return nil, "", compatibility.BadRequest("responses request must not specify both previous_response_id and conversation")
	}
	if strings.TrimSpace(dto.Conversation) != "" {
		return nil, "", compatibility.BadRequest("responses conversation is not supported in swobu v0")
	}
	if inputText == "" && len(conversation) == 0 && strings.TrimSpace(dto.PreviousResponseID) == "" {
		return nil, "", compatibility.BadRequest("responses request is missing required fields")
	}
	toolMode, err := decodeResponsesToolMode(dto.ToolChoice)
	if err != nil {
		return nil, "", err
	}
	request := compatibility.NewGenerationRequest(compatibility.GenerationRequestParams{
		Model:                strings.TrimSpace(dto.Model),
		InputText:            inputText,
		Items:                conversation,
		ToolMode:             toolMode,
		PreviousResponseID:   strings.TrimSpace(dto.PreviousResponseID),
		PromptCacheKey:       strings.TrimSpace(dto.PromptCacheKey),
		PromptCacheRetention: strings.TrimSpace(dto.PromptCacheRetention),
	})
	if err := compatibility.ValidateResponseContinuationSelectors(request); err != nil {
		return nil, "", err
	}
	return request, deliveryModeFromStream(dto.Stream), nil
}

// decodeResponsesToolMode is intentionally permissive for unknown enum/object
// values. Known values map to canonical tool modes, and unknown values degrade
// to default mode for forward compatibility. Invalid JSON type shapes still fail
// request validation.
func decodeResponsesToolMode(raw json.RawMessage) (compatibility.ToolMode, error) {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || string(raw) == "null" {
		return compatibility.ToolModeDefault, nil
	}

	var stringMode string
	if err := json.Unmarshal(raw, &stringMode); err == nil {
		switch strings.ToLower(strings.TrimSpace(stringMode)) {
		case "", "none":
			return compatibility.ToolModeDefault, nil
		case "auto":
			return compatibility.ToolModeAuto, nil
		case "required":
			return compatibility.ToolModeRequired, nil
		default:
			return compatibility.ToolModeDefault, nil
		}
	}

	var objectMode struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &objectMode); err != nil {
		return compatibility.ToolModeDefault, compatibility.BadRequest("responses request tool_choice is invalid")
	}
	switch strings.ToLower(strings.TrimSpace(objectMode.Type)) {
	case "function", "required":
		return compatibility.ToolModeRequired, nil
	case "auto":
		return compatibility.ToolModeAuto, nil
	default:
		return compatibility.ToolModeDefault, nil
	}
}

func (responsesFamilyCodec) encodeBuffered(output compatibility.CanonicalOutput) ([]byte, error) {
	encoded := make([]responsesOutputItemDTO, 0, len(output.Items()))
	outputText := ""
	for _, item := range output.Items() {
		switch item.Kind {
		case compatibility.ItemKindText:
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
		case compatibility.ItemKindToolUse:
			args, _ := json.Marshal(item.Input)
			encoded = append(encoded, responsesOutputItemDTO{
				Type:      "function_call",
				CallID:    item.ToolUseID,
				Name:      item.Name,
				Arguments: string(args),
			})
		}
	}
	return json.Marshal(responsesResponseDTO{
		ID:         fallbackID(output.ResultID(), "resp_swobu"),
		Object:     "response",
		Model:      output.Model(),
		Status:     "completed",
		OutputText: outputText,
		Output:     encoded,
		Usage:      responsesUsageFromCanonical(output.Usage()),
	})
}

func (responsesFamilyCodec) newStreamState() clientStreamEncoder {
	return &responsesClientStreamEncoder{}
}

// family's polymorphic item shapes at the transport edge.
func decodeResponsesInput(raw json.RawMessage) (string, []compatibility.CanonicalItem, error) {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil, nil
	}

	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text, nil, nil
	}

	var items []responsesInputItemDTO
	if err := json.Unmarshal(raw, &items); err != nil {
		return "", nil, compatibility.BadRequest("responses request input is invalid")
	}

	decoded := make([]compatibility.CanonicalItem, 0, len(items))
	for idx, item := range items {
		itemType := strings.TrimSpace(item.Type)
		if itemType == "" {
			itemType = "message"
		}
		switch itemType {
		case "message":
			role := strings.TrimSpace(item.Role)
			if role == "" {
				role = "user"
			}
			parts, err := decodeTextContentItems(item.Content, "responses", authorForRole(role))
			if err != nil {
				return "", nil, err
			}
			decoded = append(decoded, parts...)
		case "function_call":
			callID := strings.TrimSpace(item.CallID)
			if callID == "" {
				callID = strings.TrimSpace(item.ID)
			}
			if callID == "" {
				callID = generatedToolUseID(idx, 0)
			}
			if strings.TrimSpace(item.Name) == "" {
				return "", nil, compatibility.BadRequest("responses request function_call items require a name")
			}
			input, err := decodeJSONObject(item.Arguments, "responses request function_call arguments are invalid")
			if err != nil {
				return "", nil, err
			}
			decoded = append(decoded, compatibility.NewToolUseItem(compatibility.ItemAuthorAssistant, "", callID, strings.TrimSpace(item.Name), input))
		case "function_call_output":
			callID := strings.TrimSpace(item.CallID)
			if callID == "" {
				return "", nil, compatibility.BadRequest("responses request function_call_output items require call_id")
			}
			output, err := decodeResponseOutputText(item.Output)
			if err != nil {
				return "", nil, err
			}
			decoded = append(decoded, compatibility.NewToolResultItem(compatibility.ItemAuthorTool, callID, output))
		default:
			return "", nil, compatibility.BadRequest("responses request input contains an unsupported item type")
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
		return "", compatibility.BadRequest("responses request function_call_output is invalid")
	}
	var builder strings.Builder
	for _, part := range content {
		partType := strings.TrimSpace(part.Type)
		if partType != "" && partType != "text" && partType != "output_text" {
			return "", compatibility.BadRequest("responses request function_call_output must contain text only")
		}
		builder.WriteString(part.Text)
	}
	return builder.String(), nil
}

type responsesClientStreamEncoder struct {
	wire responsesClientStreamEncoderWire
}

func (s *responsesClientStreamEncoder) Encode(event compatibility.OutputEvent) ([][]byte, error) {
	encoder := s.encoder()
	rawFrames, err := encoder.Encode(event)
	if err != nil {
		return nil, err
	}
	frames := make([][]byte, 0, len(rawFrames))
	for _, raw := range rawFrames {
		frames = append(frames, sseData(raw))
	}
	return frames, nil
}

func (s *responsesClientStreamEncoder) Finish() ([][]byte, error) {
	encoder := s.encoder()
	rawFrames, err := encoder.Finish()
	if err != nil {
		return nil, err
	}
	frames := make([][]byte, 0, len(rawFrames))
	for _, raw := range rawFrames {
		frames = append(frames, sseData(raw))
	}
	return frames, nil
}

func (s *responsesClientStreamEncoder) encoder() *responsesClientStreamEncoderWire {
	if s.wire.toolItems == nil {
		s.wire = newResponsesClientStreamEncoderWire()
	}
	return &s.wire
}
