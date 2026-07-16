package replay

import (
	"context"
	"encoding/hex"
	"strings"
	"testing"
)

// deterministicResponseIDGenerator produces stable IDs for tests.
type deterministicResponseIDGenerator struct{}

func (deterministicResponseIDGenerator) NewResponseID(_ context.Context, exchangeID string) (ResponseID, error) {
	return ResponseID("resp_" + exchangeID), nil
}

func TestResponseIDGenerator_AllocatesStableID(t *testing.T) {
	gen := deterministicResponseIDGenerator{}
	id, err := gen.NewResponseID(context.Background(), "ex-42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id == "" {
		t.Fatal("ResponseID must not be empty")
	}
	if !strings.HasPrefix(string(id), "resp_ex-42") {
		t.Fatalf("expected prefix resp_ex-42, got %q", id)
	}
}

func TestDefaultResponseIDGenerator_AllocatesPrefixedID(t *testing.T) {
	gen := NewDefaultResponseIDGenerator()
	id, err := gen.NewResponseID(context.Background(), "ex-42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id == "" {
		t.Fatal("ResponseID must not be empty")
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
