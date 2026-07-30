package compat

import (
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestExactLoweringIsEmptyChange(t *testing.T) {
	var changes []Change
	if len(changes) != 0 {
		t.Fatalf("exact changes = %#v, want empty", changes)
	}
}

func TestChangeValidation(t *testing.T) {
	approximation := NewApproximation(
		canonical.RequestItemsMessageImageDetail,
		canonical.RequestItemsMessageImage,
		canonical.RequestPartOccurrence(canonical.RequestPartRef{Item: 1, Part: 2}),
	)
	if err := approximation.Validate(); err != nil {
		t.Fatalf("approximation validation: %v", err)
	}
	omission := NewOmission(
		canonical.ResponseItemsKind,
		canonical.ResponsePartOccurrence(canonical.ItemPosition{Item: 3}),
	)
	if err := omission.Validate(); err != nil {
		t.Fatalf("omission validation: %v", err)
	}
	if err := ValidateChanges([]Change{{Capability: canonical.RequestModel, Kind: Approximation}}); err == nil {
		t.Fatal("invalid approximation was accepted")
	}
}
