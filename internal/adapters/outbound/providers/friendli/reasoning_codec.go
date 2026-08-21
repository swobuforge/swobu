package friendli

import (
	"encoding/json"

	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// friendliChatReasoningExtractor owns Friendli's documented response member;
// its readable trace never becomes opaque continuation state.
type friendliChatReasoningExtractor struct{}

func (friendliChatReasoningExtractor) ExtractBufferedChatReasoning(message map[string]json.RawMessage) (string, error) {
	var content string
	if raw, present := message["reasoning_content"]; present && json.Unmarshal(raw, &content) != nil {
		return "", canonical.InternalError("Friendli reasoning_content is invalid")
	}
	delete(message, "reasoning_content")
	return content, nil
}

func (friendliChatReasoningExtractor) ExtractStreamedChatReasoning(delta map[string]json.RawMessage) (protocolcodec.ChatReasoningFragment, error) {
	raw, present := delta["reasoning_content"]
	if !present {
		return protocolcodec.ChatReasoningFragment{}, nil
	}
	var content string
	if err := json.Unmarshal(raw, &content); err != nil {
		return protocolcodec.ChatReasoningFragment{}, canonical.InternalError("Friendli streamed reasoning_content is invalid")
	}
	delete(delta, "reasoning_content")
	return protocolcodec.ChatReasoningFragment{Text: content, Observed: true}, nil
}

func (friendliChatReasoningExtractor) NewChatReasoningItem(content string) (canonical.CanonicalItem, error) {
	if content == "" {
		return canonical.CanonicalItem{}, nil
	}
	part, err := canonical.NewReasoningPart(canonical.ReasoningPartTrace, content)
	if err != nil {
		return canonical.CanonicalItem{}, err
	}
	return canonical.NewReasoningItem([]canonical.ReasoningPart{part}, canonical.OpaqueThinking{})
}

var _ protocolcodec.ChatReasoningExtractor = friendliChatReasoningExtractor{}
