package novita

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

// ChatReplayScope is the exact Novita Chat continuation boundary. It prevents
// another provider's opaque reasoning bytes from entering Novita history.
const ChatReplayScope canonical.ProviderChatReplayScope = "novita-chat"

type reasoningCodec struct{ standard protocolcodec.Codec }

func (c reasoningCodec) Encode(req provider.Request) (carrier.Document, []compat.Change, error) {
	document, changes, err := protocolcodec.LowerChatCompletionsRequest(req)
	if err != nil {
		return carrier.Document{}, changes, err
	}
	if err := decorateReasoningDetails(&document, req.Canonical.Items()); err != nil {
		return carrier.Document{}, changes, err
	}
	encoded, err := chatcompletions.EncodeProviderRequestDocument(document)
	return encoded, changes, err
}

type requestMessage struct {
	chatcompletions.ProviderRequestMessage
	ReasoningDetails json.RawMessage `json:"reasoning_details,omitempty"`
}

func decorateReasoningDetails(document *chatcompletions.ProviderRequestDocument, items []canonical.CanonicalItem) error {
	messages := make([]requestMessage, len(document.Messages))
	for index, message := range document.Messages {
		messages[index].ProviderRequestMessage = message
		raw, ok, err := protocolcodec.ProviderChatReplayForMessage(message, items, ChatReplayScope)
		if err != nil {
			return err
		}
		if ok {
			if !json.Valid(raw) {
				return canonical.InternalError("checkpoint contains invalid Novita Chat reasoning_details")
			}
			messages[index].ReasoningDetails = json.RawMessage(append([]byte(nil), raw...))
		}
	}
	document.Payload["messages"] = messages
	return nil
}

func (c reasoningCodec) Decode(ctx context.Context, req provider.Request, ingress provider.Ingress) (provider.DecodedResponse, error) {
	extractor := &reasoningDetailsExtractor{}
	return protocolcodec.DecodeChatWithReasoningCarrier(ctx, c.standard, req, ingress, extractor)
}

// reasoningDetailsExtractor removes Novita's two reasoning carriers before
// shared Chat decoding. The small per-response accumulator lets the shared
// stream helper construct one canonical item while retaining exact details for
// the later tool-loop replay.
type reasoningDetailsExtractor struct {
	detailsRaw  []byte
	detailItems []json.RawMessage
}

func (e *reasoningDetailsExtractor) ExtractBufferedChatReasoning(message map[string]json.RawMessage) (string, error) {
	text, err := extractReasoningText(message)
	if err != nil {
		return "", err
	}
	if raw, present := message["reasoning_details"]; present {
		if err := e.captureDetails(raw); err != nil {
			return "", err
		}
		delete(message, "reasoning_details")
	}
	return text, nil
}

func (e *reasoningDetailsExtractor) ExtractStreamedChatReasoning(delta map[string]json.RawMessage) (protocolcodec.ChatReasoningFragment, error) {
	text, err := extractReasoningText(delta)
	if err != nil {
		return protocolcodec.ChatReasoningFragment{}, err
	}
	observed := text != ""
	if raw, present := delta["reasoning_details"]; present {
		if err := e.captureDetails(raw); err != nil {
			return protocolcodec.ChatReasoningFragment{}, err
		}
		delete(delta, "reasoning_details")
		observed = true
	}
	return protocolcodec.ChatReasoningFragment{Text: text, Observed: observed}, nil
}

func extractReasoningText(fields map[string]json.RawMessage) (string, error) {
	raw, present := fields["reasoning_content"]
	if !present {
		return "", nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return "", canonical.InternalError("Novita reasoning_content is invalid")
	}
	delete(fields, "reasoning_content")
	return text, nil
}

func (e *reasoningDetailsExtractor) captureDetails(raw json.RawMessage) error {
	if !json.Valid(raw) {
		return canonical.InternalError("Novita reasoning_details is invalid JSON")
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return canonical.InternalError("Novita reasoning_details must be an array")
	}
	for _, item := range items {
		if !json.Valid(item) {
			return canonical.InternalError("Novita reasoning_details contains invalid JSON")
		}
		e.detailItems = append(e.detailItems, append(json.RawMessage(nil), item...))
	}
	// A single buffered carrier is retained byte-for-byte. Stream fragments
	// have to be recomposed into one provider array; that preserves each JSON
	// element and its order, while JSON whitespace between fragments is not a
	// meaningful provider semantic.
	if len(e.detailsRaw) == 0 && len(e.detailItems) == len(items) {
		e.detailsRaw = append([]byte(nil), raw...)
		return nil
	}
	encoded, err := json.Marshal(e.detailItems)
	if err != nil {
		return canonical.InternalError("Novita reasoning_details could not be preserved")
	}
	e.detailsRaw = encoded
	return nil
}

func (e *reasoningDetailsExtractor) NewChatReasoningItem(content string) (canonical.CanonicalItem, error) {
	if content == "" && len(e.detailItems) > 0 {
		content = detailText(e.detailItems)
	}
	if content == "" {
		return canonical.CanonicalItem{}, nil
	}
	var opaque canonical.OpaqueThinking
	if len(e.detailItems) > 0 {
		raw := e.detailsRaw
		if len(raw) == 0 {
			encoded, err := json.Marshal(e.detailItems)
			if err != nil {
				return canonical.CanonicalItem{}, canonical.InternalError("Novita reasoning_details could not be preserved")
			}
			raw = encoded
		}
		var err error
		opaque, err = canonical.NewProviderChatOpaqueThinking(ChatReplayScope, raw)
		if err != nil {
			return canonical.CanonicalItem{}, err
		}
	}
	part, err := canonical.NewReasoningPart(canonical.ReasoningPartTrace, content)
	if err != nil {
		return canonical.CanonicalItem{}, err
	}
	return canonical.NewReasoningItem([]canonical.ReasoningPart{part}, opaque)
}

func detailText(items []json.RawMessage) string {
	var text string
	for _, raw := range items {
		var item struct {
			Text string `json:"text"`
		}
		if json.Unmarshal(raw, &item) == nil {
			text += item.Text
		}
	}
	return text
}

var _ provider.Codec = reasoningCodec{}
var _ protocolcodec.ChatReasoningExtractor = (*reasoningDetailsExtractor)(nil)
