package replay

import (
	"context"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// deterministicSwobuResponseIDGenerator produces stable IDs for tests.
type deterministicSwobuResponseIDGenerator struct{}

func (deterministicSwobuResponseIDGenerator) NewSwobuResponseID(_ context.Context, exchangeID string) (canonical.SwobuResponseID, error) {
	return canonical.SwobuResponseID("resp_" + exchangeID), nil
}

func TestSwobuResponseIDGeneratorAllocatesStableID(t *testing.T) {
	gen := deterministicSwobuResponseIDGenerator{}
	swobuResponseID, err := gen.NewSwobuResponseID(context.Background(), "ex-42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if swobuResponseID == "" {
		t.Fatal("SwobuResponseID must not be empty")
	}
	if !strings.HasPrefix(string(swobuResponseID), "resp_ex-42") {
		t.Fatalf("expected prefix resp_ex-42, got %q", swobuResponseID)
	}
}

func TestDefaultSwobuResponseIDGeneratorAllocatesPrefixedID(t *testing.T) {
	gen := NewDefaultSwobuResponseIDGenerator()
	id, err := gen.NewSwobuResponseID(context.Background(), "ex-42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id == "" {
		t.Fatal("SwobuResponseID must not be empty")
	}
	if !strings.HasPrefix(string(id), "resp_") {
		t.Fatalf("expected prefix resp_, got %q", id)
	}
	suffix := strings.TrimPrefix(string(id), "resp_")
	if len(suffix) != 32 {
		t.Fatalf("expected 32 hex chars after resp_, got %q", id)
	}
	if _, err := hex.DecodeString(suffix); err != nil {
		t.Fatalf("expected hex suffix, got %q: %v", id, err)
	}
}
