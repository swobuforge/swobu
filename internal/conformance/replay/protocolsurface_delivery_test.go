package replay

import (
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/protocolsurface"
)

func toProtocolsurfaceDelivery(in delivery.Delivery) protocolsurface.Delivery {
	switch in.Mode {
	case delivery.Streaming:
		return protocolsurface.Delivery{Variant: protocolsurface.DeliveryVariantStreaming, Framing: protocolsurface.Framing(in.Framing)}
	case delivery.Buffered:
		return protocolsurface.Delivery{Variant: protocolsurface.DeliveryVariantBuffered, Framing: protocolsurface.Framing(in.Framing)}
	default:
		return protocolsurface.Delivery{Variant: protocolsurface.DeliveryVariant(""), Framing: protocolsurface.Framing(in.Framing)}
	}
}
