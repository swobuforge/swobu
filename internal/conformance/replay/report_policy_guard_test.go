package replay

import (
	"testing"

	"github.com/swobuforge/swobu/internal/exchange"
)

func TestReportPolicyGuardRejectsLossWhenNoUnreportedLossRequired(t *testing.T) {
	t.Parallel()

	err := validateLossPolicy(true, true, []exchange.ProjectionLoss{{Field: "x", Reason: "y", Severity: "z"}}, "fixture://negative/loss_present")
	if err == nil {
		t.Fatalf("expected no_loss_allowed guard failure when losses are present")
	}
}

func TestReportPolicyGuardRejectsNoticesAboveMax(t *testing.T) {
	t.Parallel()

	err := validateMaxCount("notices", 2, 1, "fixture://negative/notices_above_max")
	if err == nil {
		t.Fatalf("expected max_notices guard failure when notices exceed max")
	}
}

func TestReportPolicyGuardRejectsEvidenceAboveMax(t *testing.T) {
	t.Parallel()

	err := validateMaxCount("evidence", 2, 1, "fixture://negative/evidence_above_max")
	if err == nil {
		t.Fatalf("expected max_evidence guard failure when evidence exceeds max")
	}
}
