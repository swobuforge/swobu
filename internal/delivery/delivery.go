package delivery

import "fmt"

type Mode uint8

const (
	Buffered Mode = iota
	Streaming
)

func (m Mode) String() string {
	if m == Streaming {
		return "streaming"
	}
	return "buffered"
}

type Framing string

const (
	FramingNone      Framing = ""
	FramingSSE       Framing = "sse"
	FramingWebSocket Framing = "websocket"
	FramingNDJSON    Framing = "ndjson"
)

type Delivery struct {
	Mode    Mode
	Framing Framing
}

func (d Delivery) IsStreaming() bool {
	return d.Mode == Streaming
}

func BufferedDelivery() Delivery {
	return Delivery{Mode: Buffered, Framing: FramingNone}
}

func StreamingDelivery(framing Framing) Delivery {
	return Delivery{Mode: Streaming, Framing: framing}
}

func (d Delivery) Validate() error {
	switch d.Mode {
	case Buffered:
		if d.Framing != FramingNone {
			return fmt.Errorf("buffered delivery requires no framing")
		}
		return nil
	case Streaming:
		switch d.Framing {
		case FramingSSE, FramingWebSocket, FramingNDJSON:
			return nil
		default:
			return fmt.Errorf("streaming delivery requires explicit framing")
		}
	default:
		return fmt.Errorf("delivery mode is invalid")
	}
}
