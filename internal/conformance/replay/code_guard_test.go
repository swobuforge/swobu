package replay

import "testing"

func TestCodeGuardRejectsUnexpectedNoticeCode(t *testing.T) {
	t.Parallel()

	got := []string{"conformance.synthetic.fixture"}
	expected := []string{"conformance.capture.fixture"}

	err := validateCodeSet("notice", got, expected, "fixture://negative/unexpected_notice_code")
	if err == nil {
		t.Fatalf("expected code-set guard failure for unexpected notice code")
	}
}

func TestCodeGuardRejectsMissingExpectedEvidenceCode(t *testing.T) {
	t.Parallel()

	got := []string{""}
	expected := []string{"conformance.synthetic.replayable"}

	err := validateCodeSet("evidence", got, expected, "fixture://negative/missing_expected_evidence_code")
	if err == nil {
		t.Fatalf("expected code-set guard failure when expected evidence code is missing")
	}
}

func TestCodeGuardRejectsExtraEvidenceCode(t *testing.T) {
	t.Parallel()

	got := []string{"conformance.synthetic.replayable", "conformance.capture.replayable"}
	expected := []string{"conformance.synthetic.replayable"}

	err := validateCodeSet("evidence", got, expected, "fixture://negative/extra_evidence_code")
	if err == nil {
		t.Fatalf("expected code-set guard failure when extra evidence code appears")
	}
}
