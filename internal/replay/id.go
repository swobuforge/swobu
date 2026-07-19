package replay

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// defaultResponseIDGenerator produces opaque crypto-random IDs in production.
// The exchange path allocates them early and treats them as external lookup keys.
type defaultResponseIDGenerator struct{}

func (defaultResponseIDGenerator) NewSwobuResponseID(_ context.Context, _ string) (canonical.SwobuResponseID, error) {
	var entropy [16]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return "", fmt.Errorf("generate response id entropy: %w", err)
	}
	return canonical.SwobuResponseID(fmt.Sprintf("resp_%s", hex.EncodeToString(entropy[:]))), nil
}

// NewDefaultSwobuResponseIDGenerator returns the default crypto-random response ID
// generator suitable for production bootstrap.
func NewDefaultSwobuResponseIDGenerator() SwobuResponseIDGenerator {
	return defaultResponseIDGenerator{}
}

// SwobuResponseIDGenerator allocates stable client-visible response IDs.
type SwobuResponseIDGenerator interface {
	NewSwobuResponseID(ctx context.Context, exchangeID string) (canonical.SwobuResponseID, error)
}
