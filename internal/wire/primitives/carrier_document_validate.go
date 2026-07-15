package core

import (
	"fmt"
	"strings"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

// ValidateResponseCarrierDocument validates one carrier-native buffered response document contract.
func ValidateResponseCarrierDocument(doc carrier.CarrierDocument, expectedProtocol protocolkind.ProtocolKind) error {
	if doc.Family != expectedProtocol {
		return fmt.Errorf("wire document protocol must be %q", expectedProtocol)
	}
	if doc.Stage != carrier.StageProviderIngressIn {
		return fmt.Errorf("wire document stage must be %q", carrier.StageProviderIngressIn)
	}
	media := strings.TrimSpace(doc.Media) // swobu:io-string source=boundary
	if media != "application/json" {
		return fmt.Errorf("wire document media must be %q", "application/json")
	}
	if doc.IsEmpty() {
		return fmt.Errorf("wire document body must be configured")
	}
	return nil
}
