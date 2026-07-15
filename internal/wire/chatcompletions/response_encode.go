package chatcompletions

import (
	"encoding/json"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/effect"
	sse "github.com/swobuforge/swobu/internal/wire/framing/sse"
)

func (ResponseDocumentEncoder) EncodeResponseDocument(output canonical.CanonicalOutput) (effect.Result[carrier.CarrierDocument], error) {
	message, err := chatMessageFromOutput(output)
	if err != nil {
		return effect.Result[carrier.CarrierDocument]{}, err
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
		return effect.Result[carrier.CarrierDocument]{}, err
	}
	return effect.NewResult(carrier.NewCarrierDocument("", protocolkind.ChatCompletions, "application/json", nil, raw, carrier.Meta{})), nil
}
