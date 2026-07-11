package core

import (
	"fmt"
	"strings"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

// ValidateResponseCarrierDocument validates one carrier-native buffered response document contract.
func ValidateResponseCarrierDocument(doc carrier.WireDocument, expectedProtocol protocolkind.ProtocolKind) error {
	if doc.Family != expectedProtocol {
		return fmt.Errorf("wire document protocol must be %q", expectedProtocol)
	}
	if doc.Stage != carrier.StageProviderIngressIn {
		return fmt.Errorf("wire document stage must be %q", carrier.StageProviderIngressIn)
	}
	if strings.TrimSpace(doc.Media) != "application/json" {
		return fmt.Errorf("wire document media must be %q", "application/json")
	}
	if doc.IsEmpty() {
		return fmt.Errorf("wire document body must be configured")
	}
	return nil
}
