package modelscope

import (
	"context"
	"encoding/json"

	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
)

// reasoningCodec leaves standard Chat requests untouched and extracts only
// ModelScope's readable response trace.
type reasoningCodec struct{ standard protocolcodec.Codec }

func (c reasoningCodec) Encode(req provider.Request) (carrier.Document, []compat.Change, error) {
	return c.standard.Encode(req)
}

func (c reasoningCodec) Decode(ctx context.Context, req provider.Request, ingress provider.Ingress) (provider.DecodedResponse, error) {
	return protocolcodec.DecodeChatWithReasoningCarrier(ctx, c.standard, req, ingress, modelScopeReasoningExtractor{})
}

// modelScopeReasoningExtractor projects readable reasoning without inventing
// provider continuation state or replay semantics.
type modelScopeReasoningExtractor struct{}

func (modelScopeReasoningExtractor) ExtractBufferedChatReasoning(message map[string]json.RawMessage) (string, error) {
	var content string
	if raw, present := message["reasoning_content"]; present && json.Unmarshal(raw, &content) != nil {
		return "", canonical.InternalError("ModelScope reasoning_content is invalid")
	}
	delete(message, "reasoning_content")
	return content, nil
}

func (modelScopeReasoningExtractor) ExtractStreamedChatReasoning(delta map[string]json.RawMessage) (protocolcodec.ChatReasoningFragment, error) {
	raw, present := delta["reasoning_content"]
	if !present {
		return protocolcodec.ChatReasoningFragment{}, nil
	}
	var content string
	if err := json.Unmarshal(raw, &content); err != nil {
		return protocolcodec.ChatReasoningFragment{}, canonical.InternalError("ModelScope streamed reasoning_content is invalid")
	}
	delete(delta, "reasoning_content")
	return protocolcodec.ChatReasoningFragment{Text: content, Observed: true}, nil
}

func (modelScopeReasoningExtractor) NewChatReasoningItem(content string) (canonical.CanonicalItem, error) {
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
var _ protocolcodec.ChatReasoningExtractor = modelScopeReasoningExtractor{}
