package e2e_test

import (
	"testing"

	"github.com/metrofun/swobu/internal/domain/endpointintent"
)

func mustProviderConfigWithModelID(t *testing.T, cfg endpointintent.ProviderConfig, modelID string) endpointintent.ProviderConfig {
	t.Helper()

	next, err := cfg.WithModelID(modelID)
	if err != nil {
		t.Fatalf("WithModelID(%q) returned error: %v", modelID, err)
	}
	return next
}
