package cerebras

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

// ChatReplayScope owns Cerebras v2's exact assistant reasoning replay unit.
const ChatReplayScope canonical.ProviderChatReplayScope = "cerebras-chat"

type reasoningCodec struct{ standard protocolcodec.Codec }

func (c reasoningCodec) Encode(req provider.Request) (carrier.Document, []compat.Change, error) {
	document, changes, err := protocolcodec.LowerChatCompletionsRequest(req)
	if err != nil {
		return carrier.Document{}, changes, err
	}
	if err := decorateReasoning(&document, req.Canonical.Items()); err != nil {
		return carrier.Document{}, changes, err
	}
	encoded, err := chatcompletions.EncodeProviderRequestDocument(document)
	return encoded, changes, err
}

type requestMessage struct {
	chatcompletions.ProviderRequestMessage
	Reasoning string `json:"reasoning,omitempty"`
}

func decorateReasoning(document *chatcompletions.ProviderRequestDocument, items []canonical.CanonicalItem) error {
	messages := make([]requestMessage, len(document.Messages))
	for index, message := range document.Messages {
		messages[index].ProviderRequestMessage = message
		raw, ok, err := protocolcodec.ProviderChatReplayForMessage(message, items, ChatReplayScope)
		if err != nil {
			return err
		}
		if ok {
			messages[index].Reasoning = string(raw)
		}
	}
	document.Payload["messages"] = messages
	return nil
}

func (c reasoningCodec) Decode(ctx context.Context, req provider.Request, ingress provider.Ingress) (provider.DecodedResponse, error) {
	return protocolcodec.DecodeChatWithReasoningCarrier(ctx, c.standard, req, ingress, cerebrasChatReasoningExtractor{})
}

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

var _ provider.Codec = reasoningCodec{}
var _ protocolcodec.ChatReasoningExtractor = cerebrasChatReasoningExtractor{}
