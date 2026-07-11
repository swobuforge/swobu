package protocolsurface

import "fmt"

// DeliveryVariant names the adapter-edge delivery shape before framing is
// applied.
type DeliveryVariant string

const (
	DeliveryVariantBuffered  DeliveryVariant = "buffered"
	DeliveryVariantStreaming DeliveryVariant = "streaming"
)

func (v DeliveryVariant) String() string {
	return string(v)
}

// Framing names the transport framing used by a streaming delivery variant.
type Framing string

const (
	FramingNone      Framing = ""
	FramingSSE       Framing = "sse"
	FramingWebSocket Framing = "websocket"
	FramingNDJSON    Framing = "ndjson"
)

func (f Framing) String() string {
	return string(f)
}

// Delivery is the adapter-edge delivery value object used by app/provider
// seams.
type Delivery struct {
	Variant DeliveryVariant
	Framing Framing
}

// BufferedDelivery returns one buffered adapter-edge delivery.
func BufferedDelivery() Delivery {
	return Delivery{Variant: DeliveryVariantBuffered, Framing: FramingNone}
}

// StreamingDelivery returns one streaming adapter-edge delivery with explicit
// framing.
func StreamingDelivery(framing Framing) Delivery {
	return Delivery{Variant: DeliveryVariantStreaming, Framing: framing}
}

// IsStreaming reports whether the delivery is streaming.
func (d Delivery) IsStreaming() bool {
	return d.Variant == DeliveryVariantStreaming
}

// Validate checks the delivery variant/framing pair for adapter-edge use.
func (d Delivery) Validate() error {
	switch d.Variant {
	case DeliveryVariantBuffered:
		if d.Framing != FramingNone {
			return fmt.Errorf("buffered delivery requires no framing")
		}
		return nil
	case DeliveryVariantStreaming:
		switch d.Framing {
		case FramingSSE, FramingWebSocket, FramingNDJSON:
			return nil
		default:
			return fmt.Errorf("streaming delivery requires explicit framing")
		}
	default:
		return fmt.Errorf("delivery variant is invalid")
	}
}
