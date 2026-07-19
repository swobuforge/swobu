package responses

import (
	"encoding/json"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/wire"
	sse "github.com/swobuforge/swobu/internal/wire/framing/sse"
)

func (ResponseDocumentEncoder) EncodeResponseDocument(output canonical.CanonicalOutput) (wire.ClientDocumentResult, error) {
	encoded := make([]any, 0, len(output.Items()))
	outputText := ""
	status, incompleteReason := responsesWireStatusForFinishReason(output.FinishReason())
	for _, item := range output.Items() {
		switch item.Kind() {
		case canonical.ItemKindText:
			textItem, ok := item.TextItem()
			if !ok {
				return wire.ClientDocumentResult{}, canonical.InternalError("responses text item payload is invalid")
			}
			text := textItem.Text
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
			toolUse, ok := item.ToolUse()
			if !ok {
				return wire.ClientDocumentResult{}, canonical.InternalError("responses tool-use item payload is invalid")
			}
			encoded = append(encoded, responsesWireToolItem(
				sse.FallbackID(item.ItemID(), toolUse.UseID),
				toolUse.UseID,
				toolUse.Name,
				toolUse.ToolType,
				status,
				toolUse.Input.RawObject(),
			))
		}
	}
	encodedBody, err := json.Marshal(responsesResponseDTO{
		ID:                sse.FallbackID(output.Response().SwobuID.String(), "resp_swobu"),
		Object:            "response",
		Model:             output.Model(),
		Status:            status,
		IncompleteDetails: responsesIncompleteDetailsForStatus(status, incompleteReason),
		OutputText:        outputText,
		Output:            encoded,
		Usage:             responsesUsageFromCanonical(output.Usage()),
	})
	if err != nil {
		return wire.ClientDocumentResult{}, err
	}
	logResponsesEgressBuffered(encodedBody)
	return wire.ClientDocumentResult{Document: carrier.NewDocument(protocolkind.Responses, "application/json", nil, encodedBody, carrier.Meta{})}, nil
}

func responsesWireStatusForFinishReason(finishReason string) (string, string) {
	normalized := normalizedResponseString(finishReason)
	if normalized == "" || normalized == "completed" || normalized == "incomplete" || normalized == "stop" || normalized == "end_turn" || normalized == "tool_calls" {
		return "completed", ""
	}
	if normalized == "content_filter" || normalized == "max_output_tokens" || normalized == "length" || normalized == "refusal" || normalized == "safety" || normalized == "guardrail_intervened" || normalized == "content_filtered" {
		return "incomplete", normalized
	}
	return "completed", ""
}

func responsesIncompleteDetailsForStatus(status string, incompleteReason string) *responsesIncompleteDetailsDTO {
	if trimmedResponseString(status) != "incomplete" {
		return nil
	}
	reason := trimmedResponseString(incompleteReason)
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
