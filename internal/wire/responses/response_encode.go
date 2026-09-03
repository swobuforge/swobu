package responses

import (
	"encoding/json"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/wire"
	sse "github.com/swobuforge/swobu/internal/wire/framing/sse"
)

func (ResponseDocumentEncoder) EncodeResponseDocument(request canonical.CanonicalRequest, output canonical.CanonicalResponse) (wire.ClientDocumentResult, error) {
	encoded, outputText, status, incompleteReason, err := encodeResponsesOutput(request, output)
	if err != nil {
		return wire.ClientDocumentResult{}, err
	}
	responseFingerprint, err := fingerprintResponsesResponseValue(encoded)
	if err != nil {
		return wire.ClientDocumentResult{}, err
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
	return wire.ClientDocumentResult{
		Document:            carrier.NewDocument(protocolkind.Responses, "application/json", nil, encodedBody, carrier.Meta{}),
		ResponseFingerprint: &responseFingerprint,
	}, nil
}

func encodeResponsesOutput(request canonical.CanonicalRequest, output canonical.CanonicalResponse) ([]responsesHistoryItemDTO, string, string, string, error) {
	state := responsesResponseHistoryState{completion: output.Completion()}
	outputText := ""
	status, incompleteReason := responsesWireStatusForCompletion(output.Completion())
	for ordinal, item := range output.Items() {
		if message, ok := item.Message(); ok {
			for _, part := range message.Content() {
				if text, textOK := part.Text(); textOK {
					outputText += text.Text()
				}
			}
		}
		before := len(state.items)
		if err := state.appendItem(request, ordinal, item); err != nil {
			return nil, "", "", "", err
		}
		if len(state.items) == before {
			continue
		}
		if state.items[len(state.items)-1].Status == "" {
			state.items[len(state.items)-1].Status = status
		}
	}
	return state.items, outputText, status, incompleteReason, nil
}

func responsesWireStatusForCompletion(completion canonical.Completion) (string, string) {
	if completion.Class() == canonical.CompletionCompleted {
		return "completed", ""
	}
	return "incomplete", normalizedResponseString(completion.Reason())
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
	if !hasInput || !hasOutput {
		return nil
	}
	total := input + output
	dto := &responsesUsageDTO{
		InputTokens:  input,
		OutputTokens: output,
		TotalTokens:  total,
	}
	if hasCacheRead {
		dto.InputDetails = &responsesInputDetailsDTO{
			CachedTokens: cacheRead,
		}
		if hasCacheWrite {
			dto.InputDetails.CacheWriteTokens = &cacheWrite
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
