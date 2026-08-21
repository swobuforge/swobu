package mistral

import (
	"encoding/json"
	"fmt"

	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/wire/reasoningprojection"
)

// ChatReplayScope owns Mistral's exact Chat ThinkChunk replay state.
const ChatReplayScope canonical.ProviderChatReplayScope = "mistral-chat"

func applyMistralReasoning(req canonical.CanonicalRequest, changeLog *[]compat.Change, exchangeID string) (map[string]any, error) {
	value, present, changes := reasoningprojection.ProjectOrdinalReasoning(req.Reasoning(), req.Controls().Effort)
	if changeLog != nil {
		*changeLog = append(*changeLog, changes...)
	}
	if !present {
		return nil, nil
	}
	if value == string(canonical.InferenceEffortMax) {
		value = string(canonical.InferenceEffortXHigh)
		if changeLog != nil {
			*changeLog = compat.AppendUnique(*changeLog, compat.NewApproximation(
				canonical.RequestControlsEffort,
				canonical.RequestControlsEffort,
				canonical.Occurrence{},
			))
		}
	}
	return map[string]any{"reasoning_effort": value}, nil
}

type mistralThinkingText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type mistralThinkingChunk struct {
	Type     string                `json:"type"`
	Thinking []mistralThinkingText `json:"thinking"`
}

type mistralTextChunk struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func applyMistralMessage(message *protocolcodec.ChatProviderRequestMessage, items []canonical.CanonicalItem) error {
	raw, ok, err := protocolcodec.ProviderChatReplayForMessage(*message, items, ChatReplayScope)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	chunks := []any{mistralThinkingChunk{
		Type:     "thinking",
		Thinking: []mistralThinkingText{{Type: "text", Text: string(raw)}},
	}}
	visible, err := mistralVisibleChunks(message.Content)
	if err != nil {
		return err
	}
	chunks = append(chunks, visible...)
	message.Content = chunks
	return nil
}

func mistralVisibleChunks(content any) ([]any, error) {
	switch value := content.(type) {
	case nil:
		return nil, nil
	case string:
		if value == "" {
			return nil, nil
		}
		return []any{mistralTextChunk{Type: "text", Text: value}}, nil
	case []any:
		chunks := make([]any, 0, len(value))
		for _, part := range value {
			raw, err := json.Marshal(part)
			if err != nil {
				return nil, canonical.InternalError("Mistral assistant replay content could not be inspected")
			}
			var text mistralTextChunk
			if err := json.Unmarshal(raw, &text); err != nil || text.Type != "text" {
				return nil, canonical.InternalError("Mistral assistant replay accepts only standard assistant text content")
			}
			chunks = append(chunks, text)
		}
		return chunks, nil
	default:
		return nil, canonical.InternalError(fmt.Sprintf("Mistral assistant replay content has unsupported type %T", content))
	}
}

// mistralChatReasoningExtractor normalizes only the documented thinking/text
// content union. An unfamiliar list member fails rather than disappearing from
// the shared Chat decoder.
type mistralChatReasoningExtractor struct{}

func (mistralChatReasoningExtractor) ExtractBufferedChatReasoning(message map[string]json.RawMessage) (string, error) {
	return extractMistralContent(message, false)
}

func (mistralChatReasoningExtractor) ExtractStreamedChatReasoning(delta map[string]json.RawMessage) (protocolcodec.ChatReasoningFragment, error) {
	text, observed, err := extractMistralStreamContent(delta)
	return protocolcodec.ChatReasoningFragment{Text: text, Observed: observed}, err
}

func extractMistralContent(fields map[string]json.RawMessage, streamed bool) (string, error) {
	raw, present := fields["content"]
	if !present {
		return "", nil
	}
	var ordinary string
	if json.Unmarshal(raw, &ordinary) == nil {
		return "", nil
	}
	if string(raw) == "null" {
		return "", nil
	}
	thinking, visible, observed, err := decodeMistralContentChunks(raw)
	if err != nil {
		if streamed {
			return "", canonical.InternalError("Mistral streamed content chunks are invalid")
		}
		return "", canonical.InternalError("Mistral content chunks are invalid")
	}
	if !observed && visible == "" {
		return "", nil
	}
	encoded, err := json.Marshal(visible)
	if err != nil {
		return "", canonical.InternalError("Mistral visible content could not be normalized")
	}
	fields["content"] = encoded
	return thinking, nil
}

func extractMistralStreamContent(fields map[string]json.RawMessage) (string, bool, error) {
	raw, present := fields["content"]
	if !present {
		return "", false, nil
	}
	var ordinary string
	if json.Unmarshal(raw, &ordinary) == nil {
		return "", false, nil
	}
	if string(raw) == "null" {
		return "", false, nil
	}
	thinking, visible, observed, err := decodeMistralContentChunks(raw)
	if err != nil {
		return "", false, canonical.InternalError("Mistral streamed content chunks are invalid")
	}
	if visible == "" {
		delete(fields, "content")
	} else {
		fields["content"], _ = json.Marshal(visible)
	}
	return thinking, observed, nil
}

func decodeMistralContentChunks(raw json.RawMessage) (thinking string, visible string, observed bool, err error) {
	var chunks []json.RawMessage
	if err := json.Unmarshal(raw, &chunks); err != nil {
		return "", "", false, err
	}
	for _, rawChunk := range chunks {
		var discriminator struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(rawChunk, &discriminator); err != nil || discriminator.Type == "" {
			return "", "", false, fmt.Errorf("content chunk discriminator is invalid")
		}
		switch discriminator.Type {
		case "thinking":
			var chunk mistralThinkingChunk
			if err := json.Unmarshal(rawChunk, &chunk); err != nil {
				return "", "", false, err
			}
			observed = true
			for _, part := range chunk.Thinking {
				if part.Type != "text" {
					return "", "", false, fmt.Errorf("thinking part type %q is unsupported", part.Type)
				}
				thinking += part.Text
			}
		case "text":
			var chunk mistralTextChunk
			if err := json.Unmarshal(rawChunk, &chunk); err != nil {
				return "", "", false, err
			}
			visible += chunk.Text
		default:
			return "", "", false, fmt.Errorf("content chunk type %q is unsupported", discriminator.Type)
		}
	}
	return thinking, visible, observed, nil
}

func (mistralChatReasoningExtractor) NewChatReasoningItem(content string) (canonical.CanonicalItem, error) {
	if content == "" {
		return canonical.CanonicalItem{}, nil
	}
	opaque, err := canonical.NewProviderChatOpaqueThinking(ChatReplayScope, []byte(content))
	if err != nil {
		return canonical.CanonicalItem{}, err
	}
	part, err := canonical.NewReasoningPart(canonical.ReasoningPartTrace, content)
	if err != nil {
		return canonical.CanonicalItem{}, err
	}
	return canonical.NewReasoningItem([]canonical.ReasoningPart{part}, opaque)
}

var _ protocolcodec.ChatReasoningExtractor = mistralChatReasoningExtractor{}
