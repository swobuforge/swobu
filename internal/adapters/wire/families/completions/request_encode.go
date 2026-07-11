package completions

import (
	"encoding/json"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func EncodeCarrier(req canonical.CanonicalRequest, d delivery.Delivery) (carrier.WireDocument, error) {
	switch d.Mode {
	case delivery.Buffered, delivery.Streaming:
	default:
		return carrier.WireDocument{}, canonical.UnsupportedDelivery("prompt requests do not implement the requested delivery mode on the completions protocol")
	}

	payload := map[string]any{
		"model":  req.Model(),
		"prompt": textFromItems(req.Items()),
	}
	if d.Mode == delivery.Streaming {
		payload["stream"] = true
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return carrier.WireDocument{}, canonical.BadRequest("prompt request could not be encoded for the completions protocol")
	}

	return carrier.NewWireDocument(
		carrier.StageProviderRequestOut,
		"",
		"application/json",
		nil,
		raw,
		carrier.Meta{},
	), nil
}

func textFromItems(items []canonical.CanonicalItem) string {
	out := ""
	for _, item := range items {
		if item.Kind == canonical.ItemKindText {
			out += item.Text
		}
	}
	return out
}
