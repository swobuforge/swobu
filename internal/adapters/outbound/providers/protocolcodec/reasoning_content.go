package protocolcodec

import (
	"encoding/json"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// ReasoningContentExtractor implements the common readable Chat
// `reasoning_content` carrier without assigning provider replay semantics.
type ReasoningContentExtractor struct{}

func (ReasoningContentExtractor) ExtractBufferedChatReasoning(message map[string]json.RawMessage) (string, error) {
	var content string
	if raw, present := message["reasoning_content"]; present && json.Unmarshal(raw, &content) != nil {
		return "", canonical.InternalError("Chat reasoning_content is invalid")
	}
	delete(message, "reasoning_content")
	return content, nil
}

func (ReasoningContentExtractor) ExtractStreamedChatReasoning(delta map[string]json.RawMessage) (ChatReasoningFragment, error) {
	raw, present := delta["reasoning_content"]
	if !present {
		return ChatReasoningFragment{}, nil
	}
	var content string
	if err := json.Unmarshal(raw, &content); err != nil {
		return ChatReasoningFragment{}, canonical.InternalError("streamed Chat reasoning_content is invalid")
	}
	delete(delta, "reasoning_content")
	return ChatReasoningFragment{Text: content, Observed: true}, nil
}

func (ReasoningContentExtractor) NewChatReasoningItem(content string) (canonical.CanonicalItem, error) {
	if content == "" {
		return canonical.CanonicalItem{}, nil
	}
	part, err := canonical.NewReasoningPart(canonical.ReasoningPartTrace, content)
	if err != nil {
		return canonical.CanonicalItem{}, err
	}
	return canonical.NewReasoningItem([]canonical.ReasoningPart{part}, canonical.OpaqueThinking{})
}

var _ ChatReasoningExtractor = ReasoningContentExtractor{}
