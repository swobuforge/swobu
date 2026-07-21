package messages

import (
	"encoding/json"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/wire"
	sse "github.com/swobuforge/swobu/internal/wire/framing/sse"
)

func (ResponseDocumentEncoder) EncodeResponseDocument(request canonical.CanonicalRequest, output canonical.CanonicalResponse) (wire.ClientDocumentResult, error) {
	items := output.Items()
	content, decisions, err := messagesResponseContent(request, output)
	if err != nil {
		return wire.ClientDocumentResult{}, err
	}
	responseFingerprint, err := fingerprintMessagesResponseValue(messagesMessageDTO{Role: "assistant", Content: mustMarshalMessagesContent(content)})
	if err != nil {
		return wire.ClientDocumentResult{}, err
	}
	raw, err := json.Marshal(messagesResponseDTO{
		ID:         sse.FallbackID(output.Response().SwobuID.String(), "msg_swobu"),
		Type:       "message",
		Role:       "assistant",
		Model:      output.Model(),
		Content:    content,
		StopReason: messagesStopReasonForFinishReason(output.CompletionReason(), sse.ContainsToolUseOutput(items)),
		Usage:      messagesUsageFromCanonical(output.Usage()),
	})
	if err != nil {
		return wire.ClientDocumentResult{}, err
	}
	logMessagesEgressBuffered(raw)
	return wire.ClientDocumentResult{
		Document:            carrier.NewDocument(protocolkind.Messages, "application/json", nil, raw, carrier.Meta{}),
		Decisions:           decisions,
		ResponseFingerprint: &responseFingerprint,
	}, nil
}

func messagesResponseContent(request canonical.CanonicalRequest, output canonical.CanonicalResponse) ([]messagesResponsePartDTO, []compat.Decision, error) {
	items, decisions, err := projectMessagesWebSearchLifecycles(output.Items(), compat.ResponseItemsKind)
	if err != nil {
		return nil, nil, err
	}
	state := messagesResponseHistoryState{request: request.Clone()}
	for _, item := range items {
		if err := state.appendItem(item); err != nil {
			return nil, nil, err
		}
	}
	return state.content, decisions, nil
}

func mustMarshalMessagesContent(content []messagesResponsePartDTO) json.RawMessage {
	raw, err := json.Marshal(content)
	if err != nil {
		panic("validated Messages response content failed to marshal: " + err.Error())
	}
	return raw
}

func messagesUsageFromCanonical(usage canonical.TokenUsage) *messagesUsageDTO {
	input, hasInput := usage.InputTokens()
	output, hasOutput := usage.OutputTokens()
	cacheRead, hasCacheRead := usage.CacheReadTokens()
	cacheWrite, hasCacheWrite := usage.CacheWriteTokens()
	if !hasInput && !hasOutput && !hasCacheRead && !hasCacheWrite {
		return nil
	}
	return &messagesUsageDTO{
		InputTokens:              input,
		OutputTokens:             output,
		CacheReadInputTokens:     cacheRead,
		CacheCreationInputTokens: cacheWrite,
	}
}
