package friendli

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

// reasoningCodec owns Friendli's documented Chat reasoning spelling around the
// shared codec. Readable reasoning remains just that: this provider never
// claims `reasoning_content` is opaque continuation state.
type reasoningCodec struct{ standard protocolcodec.Codec }

func (c reasoningCodec) Encode(req provider.Request) (carrier.Document, []compat.Change, error) {
	document, changes, err := protocolcodec.LowerChatCompletionsRequest(req)
	if err != nil {
		return carrier.Document{}, changes, err
	}
	if disclosure, set := req.Canonical.Reasoning().DisclosureField().Get(); set && disclosure == canonical.ReasoningDisclosureNone {
		// Friendli needs parsing enabled before it can independently suppress the
		// readable trace. This branch is canonical-state driven; an unadmitted
		// client extension can never reach it.
		document.Payload["parse_reasoning"] = true
		document.Payload["include_reasoning"] = false
	}
	encoded, err := chatcompletions.EncodeProviderRequestDocument(document)
	return encoded, changes, err
}

func (c reasoningCodec) Decode(ctx context.Context, req provider.Request, ingress provider.Ingress) (provider.DecodedResponse, error) {
	return protocolcodec.DecodeChatWithReasoningCarrier(ctx, c.standard, req, ingress, friendliChatReasoningExtractor{})
}

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

var _ provider.Codec = reasoningCodec{}
var _ protocolcodec.ChatReasoningExtractor = friendliChatReasoningExtractor{}
