package core

import (
	"fmt"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

// ValidateResponseSSECarrierStream validates one carrier-native response stream contract.
func ValidateResponseSSECarrierStream(stream carrier.CarrierStream, expectedProtocol protocolkind.ProtocolKind) error {
	if stream.Family != expectedProtocol {
		return fmt.Errorf("wire stream protocol must be %q", expectedProtocol)
	}
	if stream.Framing != carrier.FramingSSE {
		return fmt.Errorf("wire stream framing must be %q", carrier.FramingSSE)
	}
	if stream.Frames == nil {
		return fmt.Errorf("wire stream frames must be configured")
	}
	return nil
}
