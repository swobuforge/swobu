package together

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

// reasoningCodec owns Together's documented Chat request and response spelling.
// `reasoning_content` preservation is deliberately excluded: it is opt-in
// replay state, while ordinary `reasoning` is readable response data.
type reasoningCodec struct{ standard protocolcodec.Codec }

func (c reasoningCodec) Encode(req provider.Request) (carrier.Document, []compat.Change, error) {
	if err := protocolcodec.ValidateEncodeRequest(req); err != nil {
		return carrier.Document{}, nil, err
	}
	var changes []compat.Change
	document, err := chatcompletions.LowerProviderRequestDocument(req.Canonical, req.ToolNames, req.Delivery, &changes, req.ExchangeID)
	if err != nil {
		return carrier.Document{}, changes, err
	}
	applyTogetherReasoning(document.Payload, req.Canonical)
	encoded, err := chatcompletions.EncodeProviderRequestDocument(document)
	return encoded, changes, err
}

func applyTogetherReasoning(payload map[string]any, request canonical.CanonicalRequest) {
	compute, computeSet := request.Reasoning().ComputeField().Get()
	effort, effortSet := request.Controls().Effort.Get()
	if computeSet {
		switch compute.Kind() {
		case canonical.ReasoningDisabled:
			payload["reasoning"] = map[string]bool{"enabled": false}
		case canonical.ReasoningAutomatic, canonical.ReasoningBudget:
			payload["reasoning"] = map[string]bool{"enabled": true}
		}
	}
	if effortSet && (effort == canonical.InferenceEffortLow || effort == canonical.InferenceEffortMedium || effort == canonical.InferenceEffortHigh) {
		payload["reasoning_effort"] = string(effort)
	}
}

func (c reasoningCodec) Decode(ctx context.Context, req provider.Request, ingress provider.Ingress) (provider.DecodedResponse, error) {
	return protocolcodec.DecodeChatWithReasoningCarrier(ctx, c.standard, req, ingress, togetherChatReasoningExtractor{})
}

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

var _ provider.Codec = reasoningCodec{}
var _ protocolcodec.ChatReasoningExtractor = togetherChatReasoningExtractor{}
