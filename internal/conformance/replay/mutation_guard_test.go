package replay

import (
	"testing"

	"github.com/swobuforge/swobu/internal/conformance/fixture"
	"github.com/swobuforge/swobu/internal/exchange"
)

func TestMutationGuardRejectsUnexpectedMutatedStage(t *testing.T) {
	t.Parallel()

	got := []exchange.StageReport{
		{Stage: string(exchange.StageClientHTTPIn), Mutated: false},
		{Stage: string(exchange.StageClientWireOut), Mutated: true},
	}
	expected := []string{"semantic_events"}

	err := validateMutatedStages(got, expected, "fixture://negative/unexpected_mutated_stage")
	if err == nil {
		t.Fatalf("expected guard failure when report marks mutation on a stage not listed in expected_mutated_stages")
	}
}

func TestMutationGuardRejectsContractMutationStageOutsideOrder(t *testing.T) {
	t.Parallel()

	contract := fixture.CaseContract{
		Name: "negative_contract",
		Assert: fixture.CaseAssertContract{
			ExpectedStageOrder:    []string{"client_http_in", "provider_wire_in"},
			ExpectedMutatedStages: []string{"semantic_events"},
		},
	}

	err := validateMutatedStagesSubsetOfExpectedOrder(contract)
	if err == nil {
		t.Fatalf("expected guard failure when expected_mutated_stages includes a stage missing from expected_stage_order")
	}
}
