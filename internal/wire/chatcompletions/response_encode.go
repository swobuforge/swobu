package chatcompletions

import (
	"encoding/json"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/wire"
	sse "github.com/swobuforge/swobu/internal/wire/framing/sse"
)

func (ResponseDocumentEncoder) EncodeResponseDocument(_ canonical.CanonicalRequest, output canonical.CanonicalResponse) (wire.ClientDocumentResult, error) {
	items, changes, err := projectChatCompletionsWebSearchLifecycles(output.Items())
	if err != nil {
		return wire.ClientDocumentResult{}, err
	}
	message, err := chatMessageFromItems(items)
	if err != nil {
		return wire.ClientDocumentResult{}, err
	}
	projectionDecisions, err := finalizeChatClientProjection(
		chatItemsContainReasoning(items),
		message.Content != "" || len(message.ToolCalls) > 0,
		output.Completion().Reason(),
	)
	changes = append(changes, projectionDecisions...)
	if err != nil {
		return wire.ClientDocumentResult{Changes: changes}, err
	}
	responseFingerprint, err := fingerprintChatCompletionsResponseItems(items)
	if err != nil {
		return wire.ClientDocumentResult{}, err
	}
	raw, err := json.Marshal(chatCompletionsResponseDTO[chatCompletionsBufferedChoiceDTO]{
		ID:     sse.FallbackID(output.Response().SwobuID.String(), "chatcmpl_swobu"),
		Object: "chat.completion",
		Model:  output.Model(),
		Choices: []chatCompletionsBufferedChoiceDTO{{
			Index:        0,
			Message:      message,
			FinishReason: chatClientFinishReason(output.Completion().Reason(), len(message.ToolCalls) > 0),
		}},
		Usage: chatUsageFromCanonical(output.Usage()),
	})
	if err != nil {
		return wire.ClientDocumentResult{}, err
	}
	result := wire.ClientDocumentResult{
		Document:            carrier.NewDocument(protocolkind.ChatCompletions, "application/json", nil, raw, carrier.Meta{}),
		Changes:             changes,
		ResponseFingerprint: &responseFingerprint,
	}
	return result, nil
}

// chatClientFinishReason derives terminal meaning from canonical output while
// the canonical completion retains the provider reason for diagnostics.
func chatClientFinishReason(providerReason string, hasToolCalls bool) string {
	if hasToolCalls {
		return "tool_calls"
	}
	return sse.DefaultFinishReason(providerReason, "stop")
}

func chatItemsContainReasoning(items []canonical.CanonicalItem) bool {
	for _, item := range items {
		if item.Kind() == canonical.ItemKindReasoning {
			return true
		}
	}
	return false
}

// finalizeChatClientProjection is the delivery-independent residual-validity
// rule for standard Chat output. Both encoders supply facts from their native
// traversal, receive the same loss decision, and reject empty success.
func finalizeChatClientProjection(sawReasoning, sawVisible bool, finishReason string) ([]compat.Change, error) {
	var changes []compat.Change
	if sawReasoning {
		changes = append(changes, compat.Change{
			Capability: canonical.ResponseItemsReasoning,
			Kind:       compat.Omission,
		})
	}
	// content_filter is itself an explicit client-visible non-answer terminal;
	// an empty stop remains fabricated success.
	if !sawVisible && finishReason != "content_filter" {
		return changes, canonical.NewBackendError("", 0, "backend response has no Chat Completions semantics after client projection", "")
	}
	return changes, nil
}
