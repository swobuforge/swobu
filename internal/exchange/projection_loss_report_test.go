package exchange

import (
	"testing"

	"github.com/swobuforge/swobu/internal/report"
)

func TestLossKindRequired(t *testing.T) {
	err := report.ValidateLoss(ProjectionLoss{
		Field:    "tools",
		Reason:   "unsupported_tool_removed",
		Severity: SeverityWarning,
	})
	if err == nil {
		t.Fatal("expected error for missing loss kind")
	}
}

func TestSemanticLossCannotBeNotice(t *testing.T) {
	err := report.ValidateLoss(ProjectionLoss{
		Field:    "tools",
		Kind:     LossUnrepresentableTool,
		Reason:   "unsupported_tool_removed",
		Severity: SeverityNotice,
	})
	if err == nil {
		t.Fatal("expected error for notice-severity unrepresentable tool loss")
	}
}
