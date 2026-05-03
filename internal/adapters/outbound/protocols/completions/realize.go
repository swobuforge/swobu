package completions

import (
	"bytes"
	"encoding/json"
	"net/http"

	"github.com/swobuforge/swobu/internal/adapters/outbound/protocols"
	"github.com/swobuforge/swobu/internal/domain/compatibility"
)

func Realize(request compatibility.CanonicalRequest, deliveryMode compatibility.DeliveryMode) (protocols.WireRequest, error) {
	switch typed := request.(type) {
	case compatibility.PromptCanonicalRequest:
		return Encode(typed, deliveryMode)
	default:
		return protocols.WireRequest{}, compatibility.UnsupportedOperation("completions protocol does not implement the canonical semantic request")
	}
}

type requestBody struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream,omitempty"`
}

func Encode(req compatibility.PromptCanonicalRequest, deliveryMode compatibility.DeliveryMode) (protocols.WireRequest, error) {
	switch deliveryMode {
	case compatibility.DeliveryModeBuffered, compatibility.DeliveryModeStreaming:
	default:
		return protocols.WireRequest{}, compatibility.UnsupportedDelivery("prompt requests do not implement the requested delivery mode on the completions protocol")
	}

	raw, err := json.Marshal(requestBody{
		Model:  req.Model(),
		Prompt: req.Prompt(),
		Stream: deliveryMode == compatibility.DeliveryModeStreaming,
	})
	if err != nil {
		return protocols.WireRequest{}, compatibility.BadRequest("prompt request could not be encoded for the completions protocol")
	}

	return protocols.WireRequest{
		Method:  http.MethodPost,
		Path:    "/completions",
		Body:    bytes.NewReader(raw),
		HasBody: true,
	}, nil
}
