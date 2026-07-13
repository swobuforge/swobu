package openai

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// ContentPartItem is one normalized OpenAI-style content part.
//
// The helper keeps the raw part type and common payload fields so callers can
// share one walker while preserving family-specific interpretation and errors.
type ContentPartItem struct {
	Type         string          `json:"type"`
	Text         string          `json:"text,omitempty"`
	InputText    string          `json:"input_text,omitempty"`
	OutputText   string          `json:"output_text,omitempty"`
	ID           string          `json:"id,omitempty"`
	Name         string          `json:"name,omitempty"`
	Input        json.RawMessage `json:"input,omitempty"`
	ToolUseID    string          `json:"tool_use_id,omitempty"`
	Content      json.RawMessage `json:"content,omitempty"`
	CacheControl json.RawMessage `json:"cache_control,omitempty"`
	CachePoint   json.RawMessage `json:"cachePoint,omitempty"`
}

func AuthorForRole(role string) canonical.ItemAuthor {
	normalizedRole := strings.TrimSpace(role) // swobu:io-string source=boundary // swobu:io-string source=provider-wire
	switch normalizedRole {
	case "assistant":
		return canonical.ItemAuthorAssistant
	case "tool":
		return canonical.ItemAuthorTool
	default:
		return canonical.ItemAuthorUser
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

func DecodeTextContentItems(raw json.RawMessage, surface string, author canonical.ItemAuthor) ([]canonical.CanonicalItem, error) {
	parts, err := DecodeContentParts(raw, surface+" message content is invalid")
	if err != nil {
		return nil, err
	}

	decoded := make([]canonical.CanonicalItem, 0, len(parts))
	err = WalkContentParts(parts, func(_ int, part ContentPartItem) error {
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
			decoded = append(decoded, canonical.NewTextItem(author, text))
		default:
			return canonical.BadRequest(surface + " message content contains an unsupported part type")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return decoded, nil
}
