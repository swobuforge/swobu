package together

import (
	"encoding/json"

	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// togetherChatReasoningExtractor owns Together's documented readable response
// member; it deliberately leaves unrelated replay state untouched.
type togetherChatReasoningExtractor struct{}

func (togetherChatReasoningExtractor) ExtractBufferedChatReasoning(message map[string]json.RawMessage) (string, error) {
	var reasoning string
	if raw, present := message["reasoning"]; present && json.Unmarshal(raw, &reasoning) != nil {
		return "", canonical.InternalError("Together AI reasoning is invalid")
	}
	delete(message, "reasoning")
	return reasoning, nil
}

func (togetherChatReasoningExtractor) ExtractStreamedChatReasoning(delta map[string]json.RawMessage) (protocolcodec.ChatReasoningFragment, error) {
	raw, present := delta["reasoning"]
	if !present {
		return protocolcodec.ChatReasoningFragment{}, nil
	}
	var content string
	if err := json.Unmarshal(raw, &content); err != nil {
		return protocolcodec.ChatReasoningFragment{}, canonical.InternalError("Together AI streamed reasoning is invalid")
	}
	delete(delta, "reasoning")
	return protocolcodec.ChatReasoningFragment{Text: content, Observed: true}, nil
}

func (togetherChatReasoningExtractor) NewChatReasoningItem(content string) (canonical.CanonicalItem, error) {
	if content == "" {
		return canonical.CanonicalItem{}, nil
	}
	part, err := canonical.NewReasoningPart(canonical.ReasoningPartTrace, content)
	if err != nil {
		return canonical.CanonicalItem{}, err
	}
	return canonical.NewReasoningItem([]canonical.ReasoningPart{part}, canonical.OpaqueThinking{})
}

var _ protocolcodec.ChatReasoningExtractor = togetherChatReasoningExtractor{}
