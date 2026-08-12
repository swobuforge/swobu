package kimi

import (
	"context"
	"encoding/json"

	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/wire/chatcompletions"
)

// reasoningCodec removes Kimi-only fields before shared Chat decoding, then
// restores the exact field only on the corresponding Kimi continuation turn.
type reasoningCodec struct{ standard protocolcodec.Codec }

// ChatReplayScope owns the exact Kimi Chat opaque reasoning replay dialect.
const ChatReplayScope canonical.ProviderChatReplayScope = "kimi-chat"

func (c reasoningCodec) Encode(req provider.Request) (carrier.Document, []compat.Change, error) {
	if err := protocolcodec.ValidateEncodeRequest(req); err != nil {
		return carrier.Document{}, nil, err
	}
	var changes []compat.Change
	document, err := chatcompletions.LowerProviderRequestDocument(req.Canonical, req.ToolNames, req.Delivery, &changes, req.ExchangeID)
	if err != nil {
		return carrier.Document{}, changes, err
	}
	normalizeEffort(document.Payload, req.Canonical)
	if err := decorateThinking(&document, req.Canonical.Items()); err != nil {
		return carrier.Document{}, changes, err
	}
	encoded, err := chatcompletions.EncodeProviderRequestDocument(document)
	return encoded, changes, err
}

func normalizeEffort(payload map[string]any, request canonical.CanonicalRequest) {
	effort, set := request.Controls().Effort.Get()
	if !set {
		return
	}
	if compute, set := request.Reasoning().ComputeField().Get(); set && compute.Kind() == canonical.ReasoningDisabled {
		return
	}
	switch effort {
	case canonical.InferenceEffortMinimal, canonical.InferenceEffortLow:
		payload["reasoning_effort"] = "low"
	case canonical.InferenceEffortMedium, canonical.InferenceEffortHigh:
		payload["reasoning_effort"] = "high"
	case canonical.InferenceEffortXHigh, canonical.InferenceEffortMax:
		payload["reasoning_effort"] = "max"
	}
}

type requestMessage struct {
	chatcompletions.ProviderRequestMessage
	ReasoningContent string `json:"reasoning_content,omitempty"`
}

func decorateThinking(document *chatcompletions.ProviderRequestDocument, items []canonical.CanonicalItem) error {
	messages := make([]requestMessage, len(document.Messages))
	for index, message := range document.Messages {
		messages[index].ProviderRequestMessage = message
		if message.Role != "assistant" || message.SourceStart < 0 {
			continue
		}
		for source := message.SourceStart; source < message.SourceEnd && source < len(items); source++ {
			reasoning, ok := items[source].Reasoning()
			if !ok {
				continue
			}
			raw, ok := reasoning.Opaque().ProviderChat(ChatReplayScope)
			if !ok {
				continue
			}
			if messages[index].ReasoningContent != "" {
				return canonical.InternalError("checkpoint contains duplicate Kimi Chat opaque thinking for one assistant message")
			}
			messages[index].ReasoningContent = string(raw)
		}
	}
	document.Payload["messages"] = messages
	return nil
}

func (c reasoningCodec) Decode(ctx context.Context, req provider.Request, ingress provider.Ingress) (provider.DecodedResponse, error) {
	return protocolcodec.DecodeChatWithReasoningCarrier(ctx, c.standard, req, ingress, kimiChatReasoningExtractor{})
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

var _ provider.Codec = reasoningCodec{}
var _ protocolcodec.ChatReasoningExtractor = kimiChatReasoningExtractor{}
