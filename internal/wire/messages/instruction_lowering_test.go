package messages

import (
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestFlattenInstructionsForMessagesPreservesWhitespaceAndReportsRoleLoss(t *testing.T) {
	set, err := canonical.NewInstructionSet([]canonical.Instruction{
		canonicaltest.MustInstruction(canonical.MessageRoleSystem, "  system  "),
		canonicaltest.MustInstruction(canonical.MessageRoleDeveloper, " developer\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	lowered := flattenInstructionsForMessages(set)
	if lowered.Text != "  system  \n\n developer\n" || lowered.Exact || len(lowered.Decisions) != 1 {
		t.Fatalf("lowered = %#v", lowered)
	}
}
