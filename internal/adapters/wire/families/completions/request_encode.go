package completions

import (
	"encoding/json"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func encodeRequestCarrier(request canonical.CanonicalRequest, d delivery.Delivery) (carrier.WireDocument, error) {
	items := request.Items()
	prompt := ""
	for _, item := range items {
		if item.Kind == canonical.ItemKindText {
			prompt += item.Text
		}
	}
	return EncodeCarrier(canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:       request.Model(),
		InputText:   prompt,
		CacheIntent: request.CacheIntent(),
	}), d)
}

type requestBody struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream,omitempty"`
}

func EncodeCarrier(req canonical.CanonicalRequest, d delivery.Delivery) (carrier.WireDocument, error) {
	switch d.Mode {
	case delivery.Buffered, delivery.Streaming:
	default:
		return carrier.WireDocument{}, canonical.UnsupportedDelivery("prompt requests do not implement the requested delivery mode on the completions protocol")
	}

	raw, err := json.Marshal(requestBody{
		Model:  req.Model(),
		Prompt: textFromItems(req.Items()),
		Stream: d.Mode == delivery.Streaming,
	})
	if err != nil {
		return carrier.WireDocument{}, canonical.BadRequest("prompt request could not be encoded for the completions protocol")
	}

	return carrier.WireDocument{
		Leg:   carrier.LegProviderRequestOut,
		Media: "application/json",
		Raw:   raw,
	}, nil
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
