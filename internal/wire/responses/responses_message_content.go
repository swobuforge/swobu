package responses

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	openaiwire "github.com/swobuforge/swobu/internal/wire/openai"
	shared "github.com/swobuforge/swobu/internal/wire/shared"
)

// decodeResponsesMessageContent preserves the scalar-input acceptance surface:
// an explicit empty input_text is the appendable history form of input: "".
// Other OpenAI-family codecs retain their own empty-part validity rules.
func decodeResponsesMessageContent(raw json.RawMessage, author canonical.MessageRole, imageLimits shared.ImageDecodeLimitPolicy, sink compat.Sink, exchangeID string, itemIndex int) ([]canonical.CanonicalItem, error) {
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
			if err := emitResponsesCompatibilityDecision(sink, exchangeID, compat.RequestItemsKind, compat.Drop, compat.Subject("wire:/input/"+strconv.Itoa(itemIndex)+"/content/"+strconv.Itoa(partIndex)+"/type")); err != nil {
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

func emitResponsesCompatibilityDecision(sink compat.Sink, exchangeID string, feature compat.Feature, outcome compat.Outcome, subject compat.Subject) error {
	if sink == nil {
		return nil
	}
	if subject == "" {
		return nil
	}
	if err := sink.Commit(context.Background(), exchangeID, []compat.Decision{{
		Feature: feature,
		Outcome: outcome,
		Subject: subject,
	}}); err != nil {
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

func decodeResponseOutputParts(raw json.RawMessage, itemType string, imageLimits shared.ImageDecodeLimitPolicy, sink compat.Sink, exchangeID string, itemIndex int) ([]canonical.ToolResultPart, error) {
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
			if err := emitResponsesCompatibilityDecision(sink, exchangeID, compat.RequestItemsToolResultContent, compat.Drop, compat.Subject("wire:/input/"+strconv.Itoa(itemIndex)+"/output/"+strconv.Itoa(partIndex)+"/type")); err != nil {
				return nil, err
			}
		}
	}
	if len(content) > 0 && len(parts) == 0 {
		return nil, canonical.BadRequest("responses request " + itemType + " has no surviving result content")
	}
	return parts, nil
}
