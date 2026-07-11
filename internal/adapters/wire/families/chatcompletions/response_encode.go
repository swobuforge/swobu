package chatcompletions

import (
	"encoding/json"

	sse "github.com/swobuforge/swobu/internal/adapters/wire/framing/sse"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

func (ResponseDocumentEncoder) EncodeResponseDocument(output canonical.CanonicalOutput) (carrier.WireDocument, error) {
	raw, err := json.Marshal(chatCompletionsResponseDTO{
		ID:     sse.FallbackID(output.ResultID(), "chatcmpl_swobu"),
		Object: "chat.completion",
		Model:  output.Model(),
		Choices: []chatCompletionsChoiceDTO{{
			Index:        0,
			Message:      chatMessageFromOutput(output),
			FinishReason: sse.DefaultFinishReason(output.FinishReason(), "stop"),
		}},
		Usage: chatUsageFromCanonical(output.Usage()),
	})
	if err != nil {
		return carrier.WireDocument{}, err
	}
	return carrier.NewWireDocument("", protocolkind.ChatCompletions, "application/json", nil, raw, carrier.Meta{}), nil
}
