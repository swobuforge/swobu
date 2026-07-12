package chatcompletions

import (
	"encoding/json"

	sse "github.com/swobuforge/swobu/internal/adapters/wire/framing/sse"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/exchange"
)

func (ResponseDocumentEncoder) EncodeResponseDocument(output canonical.CanonicalOutput) (exchange.Result[carrier.WireDocument], error) {
	message, err := chatMessageFromOutput(output)
	if err != nil {
		return exchange.Result[carrier.WireDocument]{}, err
	}
	raw, err := json.Marshal(chatCompletionsResponseDTO{
		ID:     sse.FallbackID(output.ResultID(), "chatcmpl_swobu"),
		Object: "chat.completion",
		Model:  output.Model(),
		Choices: []chatCompletionsChoiceDTO{{
			Index:        0,
			Message:      message,
			FinishReason: sse.DefaultFinishReason(output.FinishReason(), "stop"),
		}},
		Usage: chatUsageFromCanonical(output.Usage()),
	})
	if err != nil {
		return exchange.Result[carrier.WireDocument]{}, err
	}
	return exchange.NewResult(carrier.NewWireDocument("", protocolkind.ChatCompletions, "application/json", nil, raw, carrier.Meta{})), nil
}
