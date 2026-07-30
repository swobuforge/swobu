package responses

import (
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestFlattenInstructionsForResponsesPreservesWhitespaceAndReportsBlockLoss(t *testing.T) {
	items := []canonical.CanonicalItem{
		canonicaltest.MustInstruction(canonical.MessageRoleSystem, " first "),
		canonicaltest.MustInstruction(canonical.MessageRoleSystem, "second\n"),
	}
	lowered := flattenInstructionsForResponses(items)
	if lowered.Text != " first \n\nsecond\n" || lowered.Exact || len(lowered.Changes) != 1 {
		t.Fatalf("lowered = %#v", lowered)
	}
}
