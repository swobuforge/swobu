package core

import (
	"fmt"
	"strings"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

// ValidateResponseDocument validates one carrier-native buffered response document contract.
func ValidateResponseDocument(doc carrier.Document, expectedProtocol protocolkind.ProtocolKind) error {
	if doc.Family != expectedProtocol {
		return fmt.Errorf("wire document protocol must be %q", expectedProtocol)
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
