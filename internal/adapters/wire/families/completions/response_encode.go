package completions

import (
	"encoding/json"

	sse "github.com/swobuforge/swobu/internal/adapters/wire/framing/sse"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

func (ResponseDocumentEncoder) EncodeResponseDocument(output canonical.CanonicalOutput) (carrier.WireDocument, error) {
	for _, item := range output.Items() {
		if item.Kind != canonical.ItemKindText {
			return carrier.WireDocument{}, canonical.UnsupportedOperation("completions protocol does not support tool-bearing output items")
		}
	}
	raw, err := json.Marshal(completionsResponseDTO{
		ID:     sse.FallbackID(output.ResultID(), "cmpl_swobu"),
		Object: "text_completion",
		Model:  output.Model(),
		Choices: []completionsChoiceDTO{{
			Index:        0,
			Text:         sse.OutputText(output.Items()),
			FinishReason: sse.DefaultFinishReason(output.FinishReason(), "stop"),
		}},
		Usage: completionsUsageFromCanonical(output.Usage()),
	})
	if err != nil {
		return carrier.WireDocument{}, err
	}
	return carrier.NewWireDocument("", protocolkind.Completions, "application/json", nil, raw, carrier.Meta{}), nil
}
