package replay

import (
	"testing"

	"github.com/swobuforge/swobu/internal/exchange"
)

func TestStageOrderGuardRejectsMissingExpectedSubsequence(t *testing.T) {
	t.Parallel()

	got := []exchange.StageReport{
		{Stage: string(exchange.StageClientHTTPIn), Mutated: false},
		{Stage: string(exchange.StageProviderWireIn), Mutated: false},
		{Stage: string(exchange.StageClientHTTPOut), Mutated: false},
	}
	expected := []string{"client_http_in", "semantic_request", "client_http_out"}

	err := validateStageOrder(got, expected, "fixture://negative/missing_expected_subsequence")
	if err == nil {
		t.Fatalf("expected stage-order guard failure when required subsequence stage is missing")
	}
}

func TestStageOrderGuardRejectsOutOfOrderSubsequence(t *testing.T) {
	t.Parallel()

	got := []exchange.StageReport{
		{Stage: string(exchange.StageClientHTTPIn), Mutated: false},
		{Stage: string(exchange.StageProviderWireIn), Mutated: false},
		{Stage: string(exchange.StageSemanticRequest), Mutated: false},
		{Stage: string(exchange.StageClientHTTPOut), Mutated: false},
	}
	expected := []string{"client_http_in", "semantic_request", "provider_wire_in", "client_http_out"}

	err := validateStageOrder(got, expected, "fixture://negative/out_of_order_subsequence")
	if err == nil {
		t.Fatalf("expected stage-order guard failure when expected stage subsequence is out of order")
	}
}
