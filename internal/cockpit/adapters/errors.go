package adapters

import (
	"errors"
	"fmt"
	"strings"
)

// ErrUnsupportedCommand marks Cockpit affordances whose daemon command
// contract does not exist yet.
var ErrUnsupportedCommand = errors.New("cockpit adapter command is not supported by the daemon control plane yet")

func adapterFailure(operation string, err error) error {
	if err == nil {
		return nil
	}
	operation = strings.TrimSpace(operation) // swobu:io-string source=domain
	if operation == "" {
		return err
	}
	return fmt.Errorf("%s: %w", operation, err)
}
