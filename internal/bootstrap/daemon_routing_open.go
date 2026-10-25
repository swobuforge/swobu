package bootstrap

import (
	"context"
	"fmt"

	"github.com/swobuforge/swobu/internal/configstore"
)

// openDaemonRouting opens only the canonical routing document. Upgrade migration
// is intentionally not part of startup: a document that is not accepted by the
// current decoder is preserved and reported for explicit operator replacement.
func openDaemonRouting(ctx context.Context, path string) (*configstore.Store, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	store, err := configstore.OpenOrCreate(path)
	if err != nil {
		return nil, fmt.Errorf("open canonical routing config (migration is disabled): %w", err)
	}
	return store, nil
}

func validateDaemonModelCandidate(routingRaw, registryRaw []byte) error {
	if _, err := configstore.ValidateDocument(routingRaw); err != nil {
		return err
	}
	return nil
}

// Externally managed Nodes consume the same typed configuration without taking
// a writable lifetime lock. There is no file watcher or reconciliation writer.
func openDaemonRoutingConfigured(ctx context.Context, path string, readOnly bool) (*configstore.Store, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if readOnly {
		return configstore.OpenReadOnly(path)
	}
	return openDaemonRouting(ctx, path)
}
