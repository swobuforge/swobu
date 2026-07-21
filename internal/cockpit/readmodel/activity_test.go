package readmodel

import "testing"

func TestActivityRowValueSeparatesRouteFromResolvedTarget(t *testing.T) {
	row := ActivityRowReadModel{
		ObservedAt:    "15:31:08",
		ClientLabel:   "claude-cli/2.1.204",
		RouteLabel:    "openai",
		ProviderSpec:  "anthropic",
		ProviderModel: "claude-sonnet-4-6",
		HTTPStatus:    404,
	}
	if got, want := row.RowValue(), "15:31:08 claude-cli/2.1.204 openai → anthropic/claude-sonnet-4-6 404 0ms"; got != want {
		t.Fatalf("RowValue() = %q, want %q", got, want)
	}
}

func TestActivityRowValueDoesNotInventPartialTargetDescription(t *testing.T) {
	row := ActivityRowReadModel{ObservedAt: "15:31:08", ClientLabel: "claude", RouteLabel: "openai", ProviderSpec: "anthropic", HTTPStatus: 404}
	if got, want := row.RowValue(), "15:31:08 claude openai 404 0ms"; got != want {
		t.Fatalf("RowValue() = %q, want %q", got, want)
	}
}
