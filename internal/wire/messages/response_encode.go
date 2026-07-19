package messages

import (
	"encoding/json"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/wire"
	sse "github.com/swobuforge/swobu/internal/wire/framing/sse"
)

func (ResponseDocumentEncoder) EncodeResponseDocument(output canonical.CanonicalOutput) (wire.ClientDocumentResult, error) {
	items := output.Items()
	content := make([]messagesResponsePartDTO, 0, len(items))
	for _, item := range items {
		switch item.Kind() {
		case canonical.ItemKindText:
			text, ok := item.TextItem()
			if !ok {
				return wire.ClientDocumentResult{}, canonical.InternalError("messages text item payload is invalid")
			}
			content = append(content, messagesResponsePartDTO{Type: "text", Text: text.Text})
		case canonical.ItemKindToolUse:
			toolUse, ok := item.ToolUse()
			if !ok {
				return wire.ClientDocumentResult{}, canonical.InternalError("messages tool-use item payload is invalid")
			}
			if toolUse.ToolType != "" && toolUse.ToolType != canonical.ToolTypeFunction {
				return wire.ClientDocumentResult{}, canonical.UnsupportedOperation("messages protocol only supports function tool output")
			}
			input := map[string]any{}
			if raw := toolUse.Input.RawObject(); raw != "" {
				if err := json.Unmarshal([]byte(raw), &input); err != nil {
					return wire.ClientDocumentResult{}, canonical.BadRequest("messages tool_use input is invalid JSON object")
				}
			}
			content = append(content, messagesResponsePartDTO{
				Type:  "tool_use",
				ID:    toolUse.UseID,
				Name:  toolUse.Name,
				Input: input,
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
		StopReason: messagesStopReasonForFinishReason(output.FinishReason(), sse.ContainsToolUseOutput(items)),
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
