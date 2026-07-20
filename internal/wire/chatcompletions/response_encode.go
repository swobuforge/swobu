package chatcompletions

import (
	"encoding/json"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/wire"
	sse "github.com/swobuforge/swobu/internal/wire/framing/sse"
)

func (ResponseDocumentEncoder) EncodeResponseDocument(output canonical.CanonicalResponse) (wire.ClientDocumentResult, error) {
	message, err := chatMessageFromOutput(output)
	if err != nil {
		return wire.ClientDocumentResult{}, err
	}
	raw, err := json.Marshal(chatCompletionsResponseDTO{
		ID:     sse.FallbackID(output.Response().SwobuID.String(), "chatcmpl_swobu"),
		Object: "chat.completion",
		Model:  output.Model(),
		Choices: []chatCompletionsChoiceDTO{{
			Index:        0,
			Message:      message,
			FinishReason: sse.DefaultFinishReason(output.CompletionReason(), "stop"),
		}},
		Usage: chatUsageFromCanonical(output.Usage()),
	})
	if err != nil {
		return wire.ClientDocumentResult{}, err
	}
	return wire.ClientDocumentResult{Document: carrier.NewDocument(protocolkind.ChatCompletions, "application/json", nil, raw, carrier.Meta{})}, nil
}
