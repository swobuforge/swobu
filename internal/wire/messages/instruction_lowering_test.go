package messages

import (
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestFlattenInstructionsForMessagesPreservesWhitespaceAndReportsRoleLoss(t *testing.T) {
	items := []canonical.CanonicalItem{
		canonicaltest.MustInstruction(canonical.MessageRoleSystem, "  system  "),
		canonicaltest.MustInstruction(canonical.MessageRoleDeveloper, " developer\n"),
	}
	lowered := flattenInstructionsForMessages(items)
	if lowered.Text != "  system  \n\n developer\n" || lowered.Exact || len(lowered.Decisions) != 1 {
		t.Fatalf("lowered = %#v", lowered)
	}
}
