package relaystate

import (
	"errors"
	"fmt"
	"os"
)

// ValidateRuntimeJournal parses protected relay identity and TLS runtime state
// without changing file permissions, minting endpoint keys or changing certificates.
// The validator never returns bearer/key bytes in its error messages.
// Absence is valid until a relay endpoint has been established.
func ValidateRuntimeJournal(path string) error {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read relay runtime state: %w", err)
	}
	if _, err := decode(raw); err != nil {
		return fmt.Errorf("invalid relay runtime state: %w", err)
	}
	return nil
}
