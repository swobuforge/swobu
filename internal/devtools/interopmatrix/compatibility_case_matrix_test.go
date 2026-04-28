package interopmatrix

import (
	"strings"
	"testing"
)

func TestBuild_HasDeclaredAndOutOfBandCompatibilityCases(t *testing.T) {
	report := Build()
	if len(report.Declared) == 0 {
		t.Fatal("declared compatibility cases = 0, want non-empty")
	}
	if len(report.OutOfBand) != 0 {
		t.Fatalf("out_of_band compatibility cases = %d, want 0 when websocket transport is declared", len(report.OutOfBand))
	}
}

func TestBuild_CodexResponsesWebsocketIsDeclared(t *testing.T) {
	report := Build()
	found := false
	for _, compatibilityCase := range report.Declared {
		if compatibilityCase.Client == "codex-cli" && compatibilityCase.Family == "responses" && compatibilityCase.Transport == TransportWebsocket && compatibilityCase.Declared {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("missing codex responses websocket declared compatibility case")
	}
}

func TestGate_PassesForCurrentDeclaredBand(t *testing.T) {
	report := Build()
	if err := Gate(report); err != nil {
		t.Fatalf("Gate returned error: %v", err)
	}
}

func TestText_Summary(t *testing.T) {
	report := Build()
	text := Text(report)
	if !strings.Contains(text, "interop matrix report") {
		t.Fatalf("text = %q, want report heading", text)
	}
}
