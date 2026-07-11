package responses

import (
	"encoding/json"

	sse "github.com/swobuforge/swobu/internal/adapters/wire/framing/sse"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

func (ResponseDocumentEncoder) EncodeResponseDocument(output canonical.CanonicalOutput) (carrier.WireDocument, error) {
	encoded := make([]responsesOutputItemDTO, 0, len(output.Items()))
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
			encoded = append(encoded, responsesOutputItemDTO{
				Type:      "function_call",
				CallID:    item.ToolUseID,
				Name:      item.Name,
				Arguments: item.Input.RawObject(),
			})
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
		return carrier.WireDocument{}, err
	}
	logResponsesEgressBuffered(encodedBody)
	return carrier.NewWireDocument("", protocolkind.Responses, "application/json", nil, encodedBody, carrier.Meta{}), nil
}

func responsesUsageFromCanonical(usage canonical.TokenUsage) *responsesUsageDTO {
	input, hasInput := usage.InputTokens()
	output, hasOutput := usage.OutputTokens()
	cacheRead, hasCacheRead := usage.CacheReadTokens()
	cacheWrite, hasCacheWrite := usage.CacheWriteTokens()
	if !hasInput && !hasOutput && !hasCacheRead && !hasCacheWrite {
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
	return dto
}
