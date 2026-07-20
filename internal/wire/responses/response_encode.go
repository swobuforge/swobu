package responses

import (
	"encoding/json"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/wire"
	sse "github.com/swobuforge/swobu/internal/wire/framing/sse"
)

func (ResponseDocumentEncoder) EncodeResponseDocument(output canonical.CanonicalResponse) (wire.ClientDocumentResult, error) {
	encoded := make([]any, 0, len(output.Items()))
	outputText := ""
	status, incompleteReason := responsesWireStatusForFinishReason(output.CompletionReason())
	for _, item := range output.Items() {
		switch item.Kind() {
		case canonical.ItemKindMessage:
			message, _ := item.Message()
			parts := make([]responsesOutputTextItemDTO, 0, len(message.Content()))
			for _, part := range message.Content() {
				textPart, ok := part.Text()
				if !ok {
					return wire.ClientDocumentResult{}, canonical.UnsupportedOperation("responses image output is not implemented")
				}
				text := textPart.Text()
				outputText += text
				parts = append(parts, responsesOutputTextItemDTO{Type: "output_text", Text: text})
			}
			encoded = append(encoded, responsesOutputItemDTO{
				Type:    "message",
				Status:  status,
				Role:    string(message.Role()),
				Content: parts,
			})
		case canonical.ItemKindToolCall:
			call, _ := item.ToolCall()
			tool := call.Tool()
			name := tool.Name()
			input := ""
			if object, ok := call.Input().Object(); ok {
				input = object.String()
			} else if text, ok := call.Input().Text(); ok {
				input = text
			} else {
				return wire.ClientDocumentResult{}, canonical.InternalError("responses output tool input is invalid")
			}
			encoded = append(encoded, responsesWireToolItem(
				call.CallID().String(),
				call.CallID().String(),
				name,
				string(tool.Kind()),
				status,
				input,
			))
		default:
			return wire.ClientDocumentResult{}, canonical.UnsupportedOperation("responses output item kind is unsupported")
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
