// Package connectionidentity owns the stable reusable upstream reference shared
// by routing targets and the Connection registry. It carries no configuration,
// provider semantics, storage, or lifecycle effects.
package connectionidentity

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
)

// ID is opaque, stable and independent of labels, provider and endpoint.
// Imported MCP IDs may retain an existing lowercase URL-safe identity.
type ID struct{ value string }

func Parse(raw string) (ID, error) {
	if raw == "" {
		return ID{}, errors.New("Connection ID is required")
	}
	for _, c := range raw {
		if !(c >= 'a' && c <= 'z') && !(c >= '0' && c <= '9') && c != '-' && c != '_' {
			return ID{}, errors.New("Connection ID must be lowercase URL-safe")
		}
	}
	return ID{value: raw}, nil
}
func New() (ID, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return ID{}, err
	}
	return ID{value: "cn_" + hex.EncodeToString(raw[:])}, nil
}
func (id ID) String() string { return id.value }
