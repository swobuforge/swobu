package messages

import (
	"encoding/json"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/wire"
	sse "github.com/swobuforge/swobu/internal/wire/framing/sse"
)

func (ResponseDocumentEncoder) EncodeResponseDocument(output canonical.CanonicalResponse) (wire.ClientDocumentResult, error) {
	items := output.Items()
	content := make([]messagesResponsePartDTO, 0, len(items))
	for _, item := range items {
		switch item.Kind() {
		case canonical.ItemKindMessage:
			message, _ := item.Message()
			if message.Role() != canonical.MessageRoleAssistant {
				return wire.ClientDocumentResult{}, canonical.UnsupportedOperation("messages response items must be assistant-authored")
			}
			for _, part := range message.Content() {
				text, ok := part.Text()
				if !ok {
					return wire.ClientDocumentResult{}, canonical.UnsupportedOperation("messages response image output is not implemented")
				}
				content = append(content, messagesResponsePartDTO{Type: "text", Text: text.Text()})
			}
		case canonical.ItemKindToolCall:
			call, _ := item.ToolCall()
			tool := call.Tool()
			if tool.Kind() != canonical.ToolKindFunction {
				return wire.ClientDocumentResult{}, canonical.UnsupportedOperation("messages response only supports function tool calls")
			}
			name := tool.Name()
			object, ok := call.Input().Object()
			if !ok {
				return wire.ClientDocumentResult{}, canonical.BadRequest("messages response function call requires object input")
			}
			content = append(content, messagesResponsePartDTO{
				Type:  "tool_use",
				ID:    call.CallID().String(),
				Name:  name,
				Input: json.RawMessage(object.Bytes()),
			})
		case canonical.ItemKindToolResult:
			return wire.ClientDocumentResult{}, canonical.UnsupportedOperation("messages protocol does not support tool result output items")
		default:
			return wire.ClientDocumentResult{}, canonical.UnsupportedOperation("messages protocol does not support this output item kind")
		}
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
	return wire.ClientDocumentResult{Document: carrier.NewDocument(protocolkind.Messages, "application/json", nil, raw, carrier.Meta{})}, nil
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
