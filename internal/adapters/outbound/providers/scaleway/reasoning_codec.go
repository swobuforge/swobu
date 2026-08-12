package scaleway

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

// reasoningCodec removes Scaleway's response-only readable reasoning fields
// before shared Chat decoding, preserving them as a preceding canonical trace.
// It does not treat readable reasoning as replay state.
type reasoningCodec struct{ standard protocolcodec.Codec }

func (c reasoningCodec) Encode(req provider.Request) (carrier.Document, []compat.Change, error) {
	document, changes, err := protocolcodec.LowerChatCompletionsRequest(req)
	if err != nil {
		return carrier.Document{}, changes, err
	}
	encoded, err := chatcompletions.EncodeProviderRequestDocument(document)
	return encoded, changes, err
}

func (c reasoningCodec) Decode(ctx context.Context, req provider.Request, ingress provider.Ingress) (provider.DecodedResponse, error) {
	return protocolcodec.DecodeChatWithReasoningCarrier(ctx, c.standard, req, ingress, scalewayChatReasoningExtractor{})
}

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

var _ provider.Codec = reasoningCodec{}
var _ protocolcodec.ChatReasoningExtractor = scalewayChatReasoningExtractor{}
