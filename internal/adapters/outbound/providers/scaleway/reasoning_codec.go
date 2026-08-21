package scaleway

import (
	"encoding/json"

	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// scalewayChatReasoningExtractor owns both documented response spellings and
// combines their readable traces in provider-defined order.
type scalewayChatReasoningExtractor struct{}

func (scalewayChatReasoningExtractor) ExtractBufferedChatReasoning(message map[string]json.RawMessage) (string, error) {
	return removeScalewayReasoning(message, "reasoning_content", "reasoning")
}

func (scalewayChatReasoningExtractor) ExtractStreamedChatReasoning(delta map[string]json.RawMessage) (protocolcodec.ChatReasoningFragment, error) {
	text, err := removeScalewayReasoning(delta, "reasoning", "reasoning_content")
	if err != nil {
		return protocolcodec.ChatReasoningFragment{}, err
	}
	return protocolcodec.ChatReasoningFragment{Text: text, Observed: text != ""}, nil
}

func removeScalewayReasoning(message map[string]json.RawMessage, names ...string) (string, error) {
	var trace string
	for _, name := range names {
		if raw, present := message[name]; present {
			var value string
			if err := json.Unmarshal(raw, &value); err != nil {
				return "", canonical.InternalError("Scaleway " + name + " is invalid")
			}
			trace += value
			delete(message, name)
		}
	}
	return trace, nil
}

func (scalewayChatReasoningExtractor) NewChatReasoningItem(content string) (canonical.CanonicalItem, error) {
	if content == "" {
		return canonical.CanonicalItem{}, nil
	}
	part, err := canonical.NewReasoningPart(canonical.ReasoningPartTrace, content)
	if err != nil {
		return canonical.CanonicalItem{}, err
	}
	return canonical.NewReasoningItem([]canonical.ReasoningPart{part}, canonical.OpaqueThinking{})
}

var _ protocolcodec.ChatReasoningExtractor = scalewayChatReasoningExtractor{}
