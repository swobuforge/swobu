package fixture

import (
	"os"

	"gopkg.in/yaml.v3"
)

type CaseContract struct {
	Name          string             `yaml:"name"`
	FixtureSource string             `yaml:"fixture_source"`
	CapturedAt    string             `yaml:"captured_at"`
	CaptureRef    string             `yaml:"capture_ref"`
	Client        CasePeerContract   `yaml:"client"`
	Provider      CasePeerContract   `yaml:"provider"`
	Assert        CaseAssertContract `yaml:"assert"`
}

type CasePeerContract struct {
	Family   string `yaml:"family"`
	Delivery string `yaml:"delivery"`
}

type CaseAssertContract struct {
	EnvelopeGrammarValid  bool                `yaml:"envelope_grammar_valid"`
	NoLossAllowed         bool                `yaml:"no_loss_allowed"`
	NoUnreportedLoss      bool                `yaml:"no_unreported_loss"`
	ExpectedStageOrder    []string            `yaml:"expected_stage_order"`
	ExpectedStageApplied  map[string][]string `yaml:"expected_stage_applied"`
	ExpectedMutatedStages []string            `yaml:"expected_mutated_stages"`
	MaxNotices            int                 `yaml:"max_notices"`
	MaxEvidence           int                 `yaml:"max_evidence"`
	ExpectedNoticeCodes   []string            `yaml:"expected_notice_codes"`
	ExpectedEvidenceCodes []string            `yaml:"expected_evidence_codes"`
}

func LoadCaseContract(path string) (CaseContract, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return CaseContract{}, err
	}
	var out CaseContract
	if err := yaml.Unmarshal(raw, &out); err != nil {
		return CaseContract{}, err
	}
	return out, nil
}
