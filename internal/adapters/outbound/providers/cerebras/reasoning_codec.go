package cerebras

import (
	"encoding/json"

	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// ChatReplayScope owns Cerebras v2's exact assistant reasoning replay unit.
const ChatReplayScope canonical.ProviderChatReplayScope = "cerebras-chat"

type cerebrasChatReasoningExtractor struct{}

func (cerebrasChatReasoningExtractor) ExtractBufferedChatReasoning(message map[string]json.RawMessage) (string, error) {
	var content string
	raw, present := message["reasoning"]
	if present && json.Unmarshal(raw, &content) != nil {
		return "", canonical.InternalError("Cerebras reasoning is invalid")
	}
	delete(message, "reasoning")
	return content, nil
}

func (cerebrasChatReasoningExtractor) ExtractStreamedChatReasoning(delta map[string]json.RawMessage) (protocolcodec.ChatReasoningFragment, error) {
	raw, present := delta["reasoning"]
	if !present {
		return protocolcodec.ChatReasoningFragment{}, nil
	}
	var content string
	if err := json.Unmarshal(raw, &content); err != nil {
		return protocolcodec.ChatReasoningFragment{}, canonical.InternalError("Cerebras streamed reasoning is invalid")
	}
	delete(delta, "reasoning")
	return protocolcodec.ChatReasoningFragment{Text: content, Observed: true}, nil
}

func (cerebrasChatReasoningExtractor) NewChatReasoningItem(content string) (canonical.CanonicalItem, error) {
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

var _ protocolcodec.ChatReasoningExtractor = cerebrasChatReasoningExtractor{}
