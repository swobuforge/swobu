package responses

import (
	"encoding/json"

	sse "github.com/swobuforge/swobu/internal/adapters/wire/framing/sse"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/exchange"
)

func (ResponseDocumentEncoder) EncodeResponseDocument(output canonical.CanonicalOutput) (exchange.Result[carrier.WireDocument], error) {
	encoded := make([]any, 0, len(output.Items()))
	outputText := ""
	for _, item := range output.Items() {
		switch item.Kind {
		case canonical.ItemKindText:
			text := item.Text
			outputText += text
			encoded = append(encoded, responsesOutputItemDTO{
				Type:   "message",
				Status: "completed",
				Role:   "assistant",
				Content: []responsesOutputTextItemDTO{{
					Type: "output_text",
					Text: text,
				}},
			})
		case canonical.ItemKindToolUse:
			encoded = append(encoded, responsesWireToolItem(
				sse.FallbackID(item.ItemID, item.ToolUseID),
				item.ToolUseID,
				item.Name,
				item.ToolType,
				"completed",
				item.Input.RawObject(),
			))
		}
	}
	encodedBody, err := json.Marshal(responsesResponseDTO{
		ID:         sse.FallbackID(output.ResultID(), "resp_swobu"),
		Object:     "response",
		Model:      output.Model(),
		Status:     "completed",
		OutputText: outputText,
		Output:     encoded,
		Usage:      responsesUsageFromCanonical(output.Usage()),
	})
	if err != nil {
		return exchange.Result[carrier.WireDocument]{}, err
	}
	logResponsesEgressBuffered(encodedBody)
	return exchange.NewResult(carrier.NewWireDocument("", protocolkind.Responses, "application/json", nil, encodedBody, carrier.Meta{})), nil
}

func responsesUsageFromCanonical(usage canonical.TokenUsage) *responsesUsageDTO {
	input, hasInput := usage.InputTokens()
	output, hasOutput := usage.OutputTokens()
	reasoning, hasReasoning := usage.ReasoningTokens()
	cacheRead, hasCacheRead := usage.CacheReadTokens()
	cacheWrite, hasCacheWrite := usage.CacheWriteTokens()
	if !hasInput && !hasOutput && !hasReasoning && !hasCacheRead && !hasCacheWrite {
		return nil
	}
	dto := &responsesUsageDTO{
		InputTokens:  input,
		OutputTokens: output,
		TotalTokens:  input + output,
	}
	if hasCacheRead || hasCacheWrite {
		dto.InputDetails = &responsesInputDetailsDTO{
			CachedTokens:     cacheRead,
			CacheWriteTokens: cacheWrite,
		}
	}
	if hasReasoning {
		// Preserve provider-reported reasoning usage as a separate accounting
		// fact; do not fold it into total_tokens or drop a zero value.
		dto.OutputDetails = &responsesOutputDetailsDTO{
			ReasoningTokens: reasoning,
		}
	}
	return dto
}
