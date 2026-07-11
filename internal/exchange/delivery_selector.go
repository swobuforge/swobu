package exchange

import (
	"context"
	"strings"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/observation"
)

type Delivery = delivery.Delivery

type DeliverySelector interface {
	SelectProviderDelivery(ctx context.Context, route RouteSpec, client Delivery, observations observation.Snapshot) Delivery
}

type FixedDeliverySelector struct{}

func (FixedDeliverySelector) SelectProviderDelivery(_ context.Context, route RouteSpec, client Delivery, observations observation.Snapshot) Delivery {
	target := route.Provider
	for _, obs := range observations {
		code := strings.TrimSpace(obs.Code) // swobu:io-string source=boundary
		if strings.EqualFold(code, "delivery.websocket.unavailable") {
			for _, supported := range target.Delivery.Supported {
				if supported.Mode == delivery.Streaming && supported.Framing == delivery.FramingSSE {
					return supported
				}
			}
			return delivery.BufferedDelivery()
		}
	}
	if len(target.Delivery.Supported) > 0 {
		return target.Delivery.Preferred
	}
	return client
}
