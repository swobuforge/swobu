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
	content, changes, accounting, err := messagesResponseContent(request, output)
	if err != nil {
		return wire.ClientDocumentResult{}, err
	}
	stopReason, err := messagesStopReasonForCompletion(output.Completion(), sse.ContainsToolUseOutput(items))
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
		StopReason: stopReason,
		Usage:      messagesUsageFromCanonical(output.Usage(), accounting.WebSearchRequests, accounting.ObservedWebSearch),
	})
	if err != nil {
		return wire.ClientDocumentResult{}, err
	}
	return wire.ClientDocumentResult{
		Document:            carrier.NewDocument(protocolkind.Messages, "application/json", nil, raw, carrier.Meta{}),
		Changes:             changes,
		ResponseFingerprint: &responseFingerprint,
	}, nil
}

func messagesResponseContent(request canonical.CanonicalRequest, output canonical.CanonicalResponse) ([]messagesResponsePartDTO, []compat.Change, messagesWebSearchProjection, error) {
	projection, err := projectMessagesWebSearchLifecycles(output.Items(), canonical.ResponseItemsKind)
	if err != nil {
		return nil, nil, messagesWebSearchProjection{}, err
	}
	state := messagesResponseHistoryState{request: request.Clone()}
	for _, item := range projection.Items {
		if err := state.appendItem(item); err != nil {
			return nil, nil, messagesWebSearchProjection{}, err
		}
	}
	return state.content, projection.Changes, projection, nil
}

func mustMarshalMessagesContent(content []messagesResponsePartDTO) json.RawMessage {
	raw, err := json.Marshal(content)
	if err != nil {
		panic("validated Messages response content failed to marshal: " + err.Error())
	}
	return raw
}

func messagesUsageFromCanonical(usage canonical.TokenUsage, webSearchRequests int, observedWebSearch bool) *messagesUsageDTO {
	input, hasInput := usage.InputTokens()
	output, hasOutput := usage.OutputTokens()
	cacheRead, hasCacheRead := usage.CacheReadTokens()
	cacheWrite, hasCacheWrite := usage.CacheWriteTokens()
	if !hasInput && !hasOutput && !hasCacheRead && !hasCacheWrite && !observedWebSearch {
		return nil
	}
	out := &messagesUsageDTO{
		InputTokens:              input,
		OutputTokens:             output,
		CacheReadInputTokens:     cacheRead,
		CacheCreationInputTokens: cacheWrite,
	}
	if observedWebSearch {
		out.ServerToolUse = &messagesServerToolUsageDTO{WebSearchRequests: webSearchRequests}
	}
	return out
}

func messagesDeltaUsageFromCanonical(usage canonical.TokenUsage, webSearchRequests int, observedWebSearch bool) messagesDeltaUsageDTO {
	input, _ := usage.InputTokens()
	output, _ := usage.OutputTokens()
	cacheRead, _ := usage.CacheReadTokens()
	cacheWrite, _ := usage.CacheWriteTokens()
	out := messagesDeltaUsageDTO{
		InputTokens:              input,
		OutputTokens:             output,
		CacheReadInputTokens:     cacheRead,
		CacheCreationInputTokens: cacheWrite,
	}
	if observedWebSearch {
		out.ServerToolUse = &messagesServerToolUsageDTO{WebSearchRequests: webSearchRequests}
	}
	return out
}
