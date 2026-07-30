package responses

import (
	"encoding/json"
	"strings"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	openaiwire "github.com/swobuforge/swobu/internal/wire/openai"
	shared "github.com/swobuforge/swobu/internal/wire/shared"
)

// decodeResponsesMessageContent preserves the scalar-input acceptance surface:
// an explicit empty input_text is the appendable history form of input: "".
// Other OpenAI-family codecs retain their own empty-part validity rules.
func decodeResponsesMessageContent(raw json.RawMessage, author canonical.MessageRole, imageLimits shared.ImageDecodeLimitPolicy, changeLog *[]compat.Change, exchangeID string, itemIndex int) ([]canonical.CanonicalItem, error) {
	parts, err := openaiwire.DecodeContentParts(raw, "responses message content is invalid")
	if err != nil {
		return nil, err
	}
	content := make([]canonical.MessagePart, 0, len(parts))
	for partIndex, part := range parts {
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
			if err := appendResponsesOccurrenceChange(changeLog, exchangeID, canonical.RequestItemsKind, compat.Omission, canonical.RequestPartOccurrence(canonical.RequestPartRef{Item: uint32(itemIndex), Part: uint32(partIndex)})); err != nil {
				return nil, err
			}
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

func appendResponsesOccurrenceChange(changeLog *[]compat.Change, exchangeID string, feature canonical.CapabilityPath, outcome compat.Kind, occurrence canonical.Occurrence) error {
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

func responsesInputSubject(index int, _ string) canonical.Occurrence {
	return canonical.RequestItemOccurrence(uint32(index))
}

func decodeResponseOutputParts(raw json.RawMessage, itemType string, imageLimits shared.ImageDecodeLimitPolicy, changeLog *[]compat.Change, exchangeID string, itemIndex int) ([]canonical.ToolResultPart, error) {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return []canonical.ToolResultPart{canonical.NewTextToolResultPart(text)}, nil
	}
	var content []openaiwire.ContentPartItem
	if err := json.Unmarshal(raw, &content); err != nil {
		return nil, canonical.BadRequest("responses request " + itemType + " is invalid")
	}
	parts := make([]canonical.ToolResultPart, 0, len(content))
	for partIndex, part := range content {
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
				return nil, canonical.BadRequest("responses request " + itemType + " provider-scoped image file IDs are not portable")
			}
			image, err := openaiwire.DecodeOpenAIImage(part.ImageURL, "responses "+itemType, imageLimits, part.Detail)
			if err != nil {
				return nil, err
			}
			parts = append(parts, canonical.NewImageToolResultPart(image))
		case "input_file", "file":
			return nil, canonical.BadRequest("responses request " + itemType + " file content is not portable")
		default:
			if err := appendResponsesOccurrenceChange(changeLog, exchangeID, canonical.RequestItemsToolResultContent, compat.Omission, canonical.RequestPartOccurrence(canonical.RequestPartRef{Item: uint32(itemIndex), Part: uint32(partIndex)})); err != nil {
				return nil, err
			}
		}
	}
	if len(content) > 0 && len(parts) == 0 {
		return nil, canonical.BadRequest("responses request " + itemType + " has no surviving result content")
	}
	return parts, nil
}
