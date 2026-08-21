package kimi

import (
	"encoding/json"

	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// ChatReplayScope owns the exact Kimi Chat opaque reasoning replay dialect.
const ChatReplayScope canonical.ProviderChatReplayScope = "kimi-chat"

func applyKimiReasoning(req canonical.CanonicalRequest, changeLog *[]compat.Change, exchangeID string) (map[string]any, error) {
	controls := req.Controls()
	reasoning := req.Reasoning()
	effort, set := controls.Effort.Get()
	if !set {
		return nil, nil
	}
	if compute, set := reasoning.ComputeField().Get(); set && compute.Kind() == canonical.ReasoningDisabled {
		return nil, nil
	}
	fields := make(map[string]any)
	switch effort {
	case canonical.InferenceEffortMinimal, canonical.InferenceEffortLow:
		fields["reasoning_effort"] = "low"
	case canonical.InferenceEffortMedium, canonical.InferenceEffortHigh:
		fields["reasoning_effort"] = "high"
	case canonical.InferenceEffortXHigh, canonical.InferenceEffortMax:
		fields["reasoning_effort"] = "max"
	}
	return fields, nil
}

// kimiChatReasoningExtractor owns Kimi's response field spelling and exact
// opaque replay construction. Protocolcodec owns the Chat envelope mechanics.
type kimiChatReasoningExtractor struct{}

func (kimiChatReasoningExtractor) ExtractBufferedChatReasoning(message map[string]json.RawMessage) (string, error) {
	var content string
	raw, present := message["reasoning_content"]
	if present && json.Unmarshal(raw, &content) != nil {
		return "", canonical.InternalError("Kimi reasoning_content is invalid")
	}
	delete(message, "reasoning_content")
	return content, nil
}

func (kimiChatReasoningExtractor) ExtractStreamedChatReasoning(delta map[string]json.RawMessage) (protocolcodec.ChatReasoningFragment, error) {
	raw, present := delta["reasoning_content"]
	if !present {
		return protocolcodec.ChatReasoningFragment{}, nil
	}
	var content string
	if err := json.Unmarshal(raw, &content); err != nil {
		return protocolcodec.ChatReasoningFragment{}, canonical.InternalError("Kimi streamed reasoning_content is invalid")
	}
	delete(delta, "reasoning_content")
	return protocolcodec.ChatReasoningFragment{Text: content, Observed: true}, nil
}

func (kimiChatReasoningExtractor) NewChatReasoningItem(content string) (canonical.CanonicalItem, error) {
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

var _ protocolcodec.ChatReasoningExtractor = kimiChatReasoningExtractor{}
