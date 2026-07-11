package replay

import (
	"testing"

	"github.com/swobuforge/swobu/internal/conformance/fixture"
)

func TestContractGuardRejectsNoticeCodesAboveMax(t *testing.T) {
	t.Parallel()

	contract := fixture.CaseContract{
		Name: "negative_notice_contract",
		Assert: fixture.CaseAssertContract{
			MaxNotices:          1,
			ExpectedNoticeCodes: []string{"a", "b"},
		},
	}

	err := validateContractCodeMaxConsistency(contract)
	if err == nil {
		t.Fatalf("expected contract guard failure when expected_notice_codes exceeds max_notices")
	}
}

func TestContractGuardRejectsEvidenceCodesAboveMax(t *testing.T) {
	t.Parallel()

	contract := fixture.CaseContract{
		Name: "negative_evidence_contract",
		Assert: fixture.CaseAssertContract{
			MaxEvidence:           1,
			ExpectedEvidenceCodes: []string{"a", "b"},
		},
	}

	err := validateContractCodeMaxConsistency(contract)
	if err == nil {
		t.Fatalf("expected contract guard failure when expected_evidence_codes exceeds max_evidence")
	}
}

func TestContractGuardRejectsDuplicateNoticeCode(t *testing.T) {
	t.Parallel()

	contract := fixture.CaseContract{
		Name: "negative_duplicate_notice_code",
		Assert: fixture.CaseAssertContract{
			MaxNotices:          2,
			ExpectedNoticeCodes: []string{"dup.code", "dup.code"},
		},
	}

	err := validateContractCodeMaxConsistency(contract)
	if err == nil {
		t.Fatalf("expected contract guard failure when expected_notice_codes contains duplicate entry")
	}
}

func TestContractGuardRejectsDuplicateEvidenceCode(t *testing.T) {
	t.Parallel()

	contract := fixture.CaseContract{
		Name: "negative_duplicate_evidence_code",
		Assert: fixture.CaseAssertContract{
			MaxEvidence:           2,
			ExpectedEvidenceCodes: []string{"dup.code", "dup.code"},
		},
	}

	err := validateContractCodeMaxConsistency(contract)
	if err == nil {
		t.Fatalf("expected contract guard failure when expected_evidence_codes contains duplicate entry")
	}
}

func TestContractGuardRejectsEmptyNoticeCode(t *testing.T) {
	t.Parallel()

	contract := fixture.CaseContract{
		Name: "negative_empty_notice_code",
		Assert: fixture.CaseAssertContract{
			MaxNotices:          1,
			ExpectedNoticeCodes: []string{"   "},
		},
	}

	err := validateContractCodeMaxConsistency(contract)
	if err == nil {
		t.Fatalf("expected contract guard failure when expected_notice_codes contains empty entry")
	}
}

func TestContractGuardRejectsEmptyEvidenceCode(t *testing.T) {
	t.Parallel()

	contract := fixture.CaseContract{
		Name: "negative_empty_evidence_code",
		Assert: fixture.CaseAssertContract{
			MaxEvidence:           1,
			ExpectedEvidenceCodes: []string{""},
		},
	}

	err := validateContractCodeMaxConsistency(contract)
	if err == nil {
		t.Fatalf("expected contract guard failure when expected_evidence_codes contains empty entry")
	}
}
