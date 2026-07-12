package exchange

import (
	"context"
	"strings"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/observation"
)

type DeliverySelector interface {
	SelectProviderDelivery(ctx context.Context, client delivery.Delivery, observations observation.Snapshot) delivery.Delivery
}

type FixedDeliverySelector struct{}

func (FixedDeliverySelector) SelectProviderDelivery(_ context.Context, client delivery.Delivery, observations observation.Snapshot) delivery.Delivery {
	for _, obs := range observations {
		code := strings.TrimSpace(obs.Code) // swobu:io-string source=boundary
		if strings.EqualFold(code, "delivery.websocket.unavailable") {
			return delivery.StreamingDelivery(delivery.FramingSSE)
		}
	}
	return client
}
