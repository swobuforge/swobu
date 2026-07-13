package messages

import (
	"encoding/json"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/effect"
	sse "github.com/swobuforge/swobu/internal/wire/framing/sse"
)

func (ResponseDocumentEncoder) EncodeResponseDocument(output canonical.CanonicalOutput) (effect.Result[carrier.WireDocument], error) {
	items := output.Items()
	content := make([]messagesResponsePartDTO, 0, len(items))
	for _, item := range items {
		switch item.Kind {
		case canonical.ItemKindText:
			content = append(content, messagesResponsePartDTO{Type: "text", Text: item.Text})
		case canonical.ItemKindToolUse:
			if item.ToolType != "" && item.ToolType != canonical.ToolTypeFunction {
				return effect.Result[carrier.WireDocument]{}, canonical.UnsupportedOperation("messages protocol only supports function tool output")
			}
			input := map[string]any{}
			if raw := item.Input.RawObject(); raw != "" {
				if err := json.Unmarshal([]byte(raw), &input); err != nil {
					return effect.Result[carrier.WireDocument]{}, canonical.BadRequest("messages tool_use input is invalid JSON object")
				}
			}
			content = append(content, messagesResponsePartDTO{
				Type:  "tool_use",
				ID:    item.ToolUseID,
				Name:  item.Name,
				Input: input,
			})
		case canonical.ItemKindToolResult:
			return effect.Result[carrier.WireDocument]{}, canonical.UnsupportedOperation("messages protocol does not support tool result output items")
		default:
			return effect.Result[carrier.WireDocument]{}, canonical.UnsupportedOperation("messages protocol does not support this output item kind")
		}
	}
	raw, err := json.Marshal(messagesResponseDTO{
		ID:         sse.FallbackID(output.ResultID(), "msg_swobu"),
		Type:       "message",
		Role:       "assistant",
		Model:      output.Model(),
		Content:    content,
		StopReason: messagesStopReasonForFinishReason(output.FinishReason(), sse.ContainsToolUseOutput(items)),
		Usage:      messagesUsageFromCanonical(output.Usage()),
	})
	if err != nil {
		return effect.Result[carrier.WireDocument]{}, err
	}
	logMessagesEgressBuffered(raw)
	return effect.NewResult(carrier.NewWireDocument("", protocolkind.Messages, "application/json", nil, raw, carrier.Meta{})), nil
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
