package openaicompat

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

type TextContentPart struct {
	Type       string `json:"type"`
	Text       string `json:"text"`
	InputText  string `json:"input_text"`
	OutputText string `json:"output_text"`
}

func AuthorForRole(role string) canonical.ItemAuthor {
	switch strings.TrimSpace(role) { // trimlowerlint:allow boundary canonicalization
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

func DecodeTextContentItems(raw json.RawMessage, surface string, author canonical.ItemAuthor) ([]canonical.CanonicalItem, error) {
	if len(strings.TrimSpace(string(raw))) == 0 || string(raw) == "null" { // trimlowerlint:allow boundary canonicalization
		return nil, nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		if text == "" {
			return nil, nil
		}
		return []canonical.CanonicalItem{canonical.NewTextItem(author, text)}, nil
	}

	var parts []TextContentPart
	if err := json.Unmarshal(raw, &parts); err != nil {
		return nil, canonical.BadRequest(surface + " message content is invalid")
	}

	decoded := make([]canonical.CanonicalItem, 0, len(parts))
	for _, part := range parts {
		partType := strings.TrimSpace(part.Type) // trimlowerlint:allow boundary canonicalization
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
				return nil, canonical.BadRequest(surface + " text parts must not be empty")
			}
			decoded = append(decoded, canonical.NewTextItem(author, text))
		default:
			return nil, canonical.BadRequest(surface + " message content contains an unsupported part type")
		}
	}
	return decoded, nil
}
