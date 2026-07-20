package responses

import (
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestFlattenInstructionsForResponsesPreservesWhitespaceAndReportsBlockLoss(t *testing.T) {
	set, err := canonical.NewInstructionSet([]canonical.Instruction{
		canonicaltest.MustInstruction(canonical.MessageRoleSystem, " first "),
		canonicaltest.MustInstruction(canonical.MessageRoleSystem, "second\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	lowered := flattenInstructionsForResponses(set)
	if lowered.Text != " first \n\nsecond\n" || lowered.Exact || len(lowered.Decisions) != 1 {
		t.Fatalf("lowered = %#v", lowered)
	}
}
