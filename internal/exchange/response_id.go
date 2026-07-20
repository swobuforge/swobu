package exchange

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// defaultResponseIDGenerator produces opaque crypto-random IDs in production.
// Exchange allocates response identity before provider execution and checkpoint
// commit; session storage only indexes the identity after it is bound.
type defaultResponseIDGenerator struct{}

func (defaultResponseIDGenerator) NewSwobuResponseID(_ context.Context, _ string) (canonical.SwobuResponseID, error) {
	var entropy [16]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return "", fmt.Errorf("generate response id entropy: %w", err)
	}
	return canonical.SwobuResponseID(fmt.Sprintf("resp_%s", hex.EncodeToString(entropy[:]))), nil
}

// NewDefaultResponseIDGenerator returns the production response-ID generator.
func NewDefaultResponseIDGenerator() ResponseIDGenerator {
	return defaultResponseIDGenerator{}
}

// ResponseIDGenerator allocates stable client-visible response IDs before
// provider execution begins.
type ResponseIDGenerator interface {
	NewSwobuResponseID(ctx context.Context, exchangeID string) (canonical.SwobuResponseID, error)
}
