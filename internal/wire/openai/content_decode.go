package openai

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/wire/shared"
)

// ContentPartItem is one normalized OpenAI-style content part.
//
// The helper keeps the raw part type and common payload fields so callers can
// share one walker while preserving family-specific interpretation and errors.
type ContentPartItem struct {
	Type         string                `json:"type"`
	Text         string                `json:"text,omitempty"`
	InputText    string                `json:"input_text,omitempty"`
	OutputText   string                `json:"output_text,omitempty"`
	ID           string                `json:"id,omitempty"`
	Name         string                `json:"name,omitempty"`
	Input        json.RawMessage       `json:"input,omitempty"`
	ToolUseID    string                `json:"tool_use_id,omitempty"`
	IsError      bool                  `json:"is_error,omitempty"`
	Content      json.RawMessage       `json:"content,omitempty"`
	CacheControl json.RawMessage       `json:"cache_control,omitempty"`
	CachePoint   json.RawMessage       `json:"cachePoint,omitempty"`
	ImageURL     json.RawMessage       `json:"image_url,omitempty"`
	FileID       string                `json:"file_id,omitempty"`
	Detail       canonical.ImageDetail `json:"detail,omitempty"`
	Source       json.RawMessage       `json:"source,omitempty"`
	Thinking     string                `json:"thinking,omitempty"`
	Signature    string                `json:"signature,omitempty"`
	Data         string                `json:"data,omitempty"`
	Annotations  json.RawMessage       `json:"annotations,omitempty"`
	Citations    json.RawMessage       `json:"citations,omitempty"`
}

func AuthorForRole(role string) canonical.MessageRole {
	normalizedRole := strings.TrimSpace(role) // swobu:io-string source=boundary // swobu:io-string source=provider-wire
	switch normalizedRole {
	case "assistant":
		return canonical.MessageRoleAssistant
	case "system":
		return canonical.MessageRoleSystem
	case "developer":
		return canonical.MessageRoleDeveloper
	case "user":
		return canonical.MessageRoleUser
	case "tool":
		return ""
	default:
		return canonical.MessageRoleUser
	}
}

func GeneratedToolUseID(msgIdx int, partIdx int) string {
	return "toolu_swobu_" + strconv.Itoa(msgIdx) + "_" + strconv.Itoa(partIdx)
}

// DecodeContentParts normalizes one OpenAI-style content payload into walker
// records without interpreting part types.
func DecodeContentParts(raw json.RawMessage, invalidMessage string) ([]ContentPartItem, error) {
	if len(strings.TrimSpace(string(raw))) == 0 || string(raw) == "null" { // swobu:io-string source=boundary
		return nil, nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		if text == "" {
			return nil, nil
		}
		return []ContentPartItem{{Type: "text", Text: text}}, nil
	}

	var parts []ContentPartItem
	if err := json.Unmarshal(raw, &parts); err != nil {
		return nil, canonical.BadRequest(invalidMessage)
	}
	return parts, nil
}

// WalkContentParts visits decoded content parts in order and threads the part
// index through the callback so callers can keep family-specific policy.
func WalkContentParts(parts []ContentPartItem, visit func(int, ContentPartItem) error) error {
	for idx, part := range parts {
		if err := visit(idx, part); err != nil {
			return err
		}
	}
	return nil
}

func DecodeTextContentItems(raw json.RawMessage, surface string, author canonical.MessageRole, limits shared.ImageDecodeLimitPolicy, onUnknown func(int, string) error) ([]canonical.CanonicalItem, error) {
	parts, err := DecodeContentParts(raw, surface+" message content is invalid")
	if err != nil {
		return nil, err
	}

	content := make([]canonical.MessagePart, 0, len(parts))
	err = WalkContentParts(parts, func(index int, part ContentPartItem) error {
		partType := strings.TrimSpace(part.Type) // swobu:io-string source=boundary // swobu:io-string source=provider-wire
		if partType == "" {
			partType = "text"
		}
		switch partType {
		case "text", "input_text", "output_text":
			text := part.Text
			if text == "" {
				text = part.InputText
			}
			if text == "" {
				text = part.OutputText
			}
			if text == "" {
				return canonical.BadRequest(surface + " text parts must not be empty")
			}
			content = append(content, canonical.NewTextMessagePart(text))
		case "image_url", "input_image":
			if author != canonical.MessageRoleUser {
				return canonical.BadRequest(surface + " image input is only valid in user messages")
			}
			if strings.TrimSpace(part.FileID) != "" { // swobu:io-string source=provider-wire
				return canonical.BadRequest(surface + " provider-scoped image file IDs are not portable")
			}
			image, err := DecodeOpenAIImage(part.ImageURL, surface, limits, part.Detail)
			if err != nil {
				return err
			}
			content = append(content, canonical.NewImageMessagePart(image))
		default:
			if onUnknown != nil {
				return onUnknown(index, partType)
			}
			return nil
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(content) == 0 {
		return nil, nil
	}
	message, err := canonical.NewMessageItem(author, content)
	if err != nil {
		return nil, canonical.BadRequest(surface + " message author is invalid")
	}
	return []canonical.CanonicalItem{message}, nil
}

// DecodeOpenAIImage decodes one URL or data-URL image leaf without fetching
// remote content. The caller retains ownership of message or tool-result placement.
func DecodeOpenAIImage(raw json.RawMessage, surface string, limits shared.ImageDecodeLimitPolicy, siblingDetail ...canonical.ImageDetail) (canonical.ImagePart, error) {
	var wireURL string
	var detail canonical.ImageDetail
	if err := json.Unmarshal(raw, &wireURL); err != nil {
		var object struct {
			URL    string                `json:"url"`
			Detail canonical.ImageDetail `json:"detail,omitempty"`
			FileID string                `json:"file_id,omitempty"`
		}
		if err := json.Unmarshal(raw, &object); err != nil {
			return canonical.ImagePart{}, canonical.BadRequest(surface + " image URL is invalid")
		}
		wireURL = object.URL
		detail = object.Detail
		if strings.TrimSpace(object.FileID) != "" { // swobu:io-string source=provider-wire
			return canonical.ImagePart{}, canonical.BadRequest(surface + " provider-scoped image file IDs are not portable")
		}
	}
	if len(siblingDetail) > 0 && siblingDetail[0] != "" {
		detail = siblingDetail[0]
	}
	canonicalDetail := canonical.Unspecified[canonical.ImageDetail]()
	if detail != "" && detail != "auto" {
		canonicalDetail = canonical.Specify(detail)
	}
	if strings.HasPrefix(wireURL, "data:") {
		mediaType, encoded, ok := strings.Cut(strings.TrimPrefix(wireURL, "data:"), ";base64,")
		if !ok {
			return canonical.ImagePart{}, canonical.BadRequest(surface + " image data URL must be base64")
		}
		media, err := shared.NormalizeImageMediaType(mediaType)
		if err != nil {
			return canonical.ImagePart{}, canonical.BadRequest(surface + " image data URL media type is unsupported")
		}
		data, err := shared.DecodeBase64Limited(encoded, limits.MaxInlineBytes)
		if err != nil {
			return canonical.ImagePart{}, canonical.BadRequest(surface + " image data URL is invalid")
		}
		image, err := canonical.NewInlineImage(media, data, canonicalDetail)
		if err != nil {
			return canonical.ImagePart{}, canonical.BadRequest(fmt.Sprintf("%s image is invalid: %v", surface, err))
		}
		return image, nil
	}
	image, err := canonical.NewURLImage(wireURL, canonicalDetail)
	if err != nil {
		return canonical.ImagePart{}, canonical.BadRequest(surface + " image URL is invalid")
	}
	return image, nil
}

// EncodeOpenAIImageURL lowers one portable canonical image leaf to a URL or
// data URL accepted by OpenAI-style protocols.
func EncodeOpenAIImageURL(image canonical.ImagePart) (string, canonical.ImageDetail, error) {
	source := image.Source()
	if rawURL, ok := source.URL(); ok {
		detail, _ := image.Detail().Get()
		return rawURL.String(), detail, nil
	}
	if media, ok := source.Inline(); ok {
		detail, _ := image.Detail().Get()
		return "data:" + string(media.MediaType()) + ";base64," + base64.StdEncoding.EncodeToString(media.Data()), detail, nil
	}
	return "", "", fmt.Errorf("canonical image source is invalid")
}
