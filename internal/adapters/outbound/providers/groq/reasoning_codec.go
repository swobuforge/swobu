package groq

import (
	"encoding/json"

	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// groqChatReasoningExtractor owns Groq's documented readable Chat reasoning
// response member. It never turns readable trace text into opaque replay state.
type groqChatReasoningExtractor struct{}

func (groqChatReasoningExtractor) ExtractBufferedChatReasoning(message map[string]json.RawMessage) (string, error) {
	var content string
	if raw, present := message["reasoning"]; present && json.Unmarshal(raw, &content) != nil {
		return "", canonical.InternalError("Groq reasoning is invalid")
	}
	delete(message, "reasoning")
	return content, nil
}

func (groqChatReasoningExtractor) ExtractStreamedChatReasoning(delta map[string]json.RawMessage) (protocolcodec.ChatReasoningFragment, error) {
	raw, present := delta["reasoning"]
	if !present {
		return protocolcodec.ChatReasoningFragment{}, nil
	}
	var content string
	if err := json.Unmarshal(raw, &content); err != nil {
		return protocolcodec.ChatReasoningFragment{}, canonical.InternalError("Groq streamed reasoning is invalid")
	}
	delete(delta, "reasoning")
	return protocolcodec.ChatReasoningFragment{Text: content, Observed: true}, nil
}

func (groqChatReasoningExtractor) NewChatReasoningItem(content string) (canonical.CanonicalItem, error) {
	if content == "" {
		return canonical.CanonicalItem{}, nil
	}
	part, err := canonical.NewReasoningPart(canonical.ReasoningPartTrace, content)
	if err != nil {
		return canonical.CanonicalItem{}, err
	}
	return canonical.NewReasoningItem([]canonical.ReasoningPart{part}, canonical.OpaqueThinking{})
}

var _ protocolcodec.ChatReasoningExtractor = groqChatReasoningExtractor{}
