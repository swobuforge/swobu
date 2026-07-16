package replay

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// ResponseID is the client-visible identity for one completed response.
// It is allocated early and may be streamed before replay capture completes.
type ResponseID string

// ID is the store-local lookup key for one replay record.
// It is derived from ResponseID but kept as a separate type so provider IDs
// cannot casually enter the store path.
type ID string

// ReplayIDFromResponseID converts a client-visible response ID into a store
// lookup ID. P0 may use the same raw value; the type separation is the guard.
func ReplayIDFromResponseID(id ResponseID) ID {
	return ID(id)
}

// defaultResponseIDGenerator produces opaque crypto-random IDs in production.
// The exchange path allocates them early and treats them as external lookup keys.
type defaultResponseIDGenerator struct{}

func (defaultResponseIDGenerator) NewResponseID(_ context.Context, _ string) (ResponseID, error) {
	var entropy [16]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return "", fmt.Errorf("generate response id entropy: %w", err)
	}
	return ResponseID(fmt.Sprintf("resp_%s", hex.EncodeToString(entropy[:]))), nil
}

// NewDefaultResponseIDGenerator returns the default crypto-random response ID
// generator suitable for production bootstrap.
func NewDefaultResponseIDGenerator() ResponseIDGenerator {
	return defaultResponseIDGenerator{}
}

// ResponseIDGenerator allocates stable client-visible response IDs.
type ResponseIDGenerator interface {
	NewResponseID(ctx context.Context, exchangeID string) (ResponseID, error)
}
