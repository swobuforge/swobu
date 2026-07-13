package responses

import (
	"encoding/json"
	"strings"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/effect"
	sse "github.com/swobuforge/swobu/internal/wire/framing/sse"
)

func (ResponseDocumentEncoder) EncodeResponseDocument(output canonical.CanonicalOutput) (effect.Result[carrier.WireDocument], error) {
	encoded := make([]any, 0, len(output.Items()))
	outputText := ""
	status, incompleteReason := responsesWireStatusForFinishReason(output.FinishReason())
	for _, item := range output.Items() {
		switch item.Kind {
		case canonical.ItemKindText:
			text := item.Text
			outputText += text
			encoded = append(encoded, responsesOutputItemDTO{
				Type:   "message",
				Status: status,
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
				status,
				item.Input.RawObject(),
			))
		}
	}
	encodedBody, err := json.Marshal(responsesResponseDTO{
		ID:                sse.FallbackID(output.ResultID(), "resp_swobu"),
		Object:            "response",
		Model:             output.Model(),
		Status:            status,
		IncompleteDetails: responsesIncompleteDetailsForStatus(status, incompleteReason),
		OutputText:        outputText,
		Output:            encoded,
		Usage:             responsesUsageFromCanonical(output.Usage()),
	})
	if err != nil {
		return effect.Result[carrier.WireDocument]{}, err
	}
	logResponsesEgressBuffered(encodedBody)
	return effect.NewResult(carrier.NewWireDocument("", protocolkind.Responses, "application/json", nil, encodedBody, carrier.Meta{})), nil
}

func responsesWireStatusForFinishReason(finishReason string) (string, string) {
	normalized := strings.ToLower(strings.TrimSpace(finishReason)) // swobu:io-string source=boundary
	switch normalized {
	case "", "completed", "incomplete", "stop", "end_turn", "tool_calls":
		return "completed", ""
	case "content_filter", "max_output_tokens", "length", "refusal", "safety", "guardrail_intervened", "content_filtered":
		return "incomplete", normalized
	default:
		return "completed", ""
	}
}

func responsesIncompleteDetailsForStatus(status string, incompleteReason string) *responsesIncompleteDetailsDTO {
	if strings.TrimSpace(status) != "incomplete" {
		return nil
	}
	reason := strings.TrimSpace(incompleteReason)
	if reason == "" {
		return nil
	}
	return &responsesIncompleteDetailsDTO{Reason: reason}
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
