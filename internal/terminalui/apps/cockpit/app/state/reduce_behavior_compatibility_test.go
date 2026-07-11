package state

import (
	"strings"
	"testing"

	stateeffect "github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state/effect"
	stateModel "github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state/model"
)

func TestReduce_ExchangeDiagnosticsCopyRequested_IncludesStageContextWhenPresent(t *testing.T) {
	model := Model{
		ControlPlane: &ControlPlaneMismatch{
			ExpectedProtocol:  7,
			DaemonProtocol:    6,
			HasDaemonProtocol: true,
			TUIVersion:        "1.2.3",
			DaemonVersion:     "1.2.2",
		},
		TrafficRows: []TrafficRow{{
			StageReports: []stateModel.StageReport{{
				Stage:   "provider.wire.out",
				Carrier: "wire_document",
				Applied: []string{"openaifamily.CacheAffinityWireTransform"},
				Mutated: true,
			}},
		}},
	}

	effects := Reduce(&model, ExchangeDiagnosticsCopyRequested{})
	if len(effects) != 1 {
		t.Fatalf("effects len=%d want 1", len(effects))
	}
	copyEff, ok := effects[0].(stateeffect.CopyExchangeDiagnosticsEffect)
	if !ok {
		t.Fatalf("effect type=%T want CopyExchangeDiagnosticsEffect", effects[0])
	}
	if !strings.Contains(copyEff.Text, "protocol mismatch: expected 7, got 6") {
		t.Fatalf("copy text missing mismatch line: %q", copyEff.Text)
	}
	if !strings.Contains(copyEff.Text, "exchange stages:") {
		t.Fatalf("copy text missing stage section: %q", copyEff.Text)
	}
	if !strings.Contains(copyEff.Text, "- provider.wire.out [wire_document] mutated (openaifamily.CacheAffinityWireTransform)") {
		t.Fatalf("copy text missing stage line: %q", copyEff.Text)
	}
}

func TestReduce_ExchangeDiagnosticsCopyRequested_WithoutStageContextKeepsBaseDiagnostics(t *testing.T) {
	model := Model{
		ControlPlane: &ControlPlaneMismatch{
			ExpectedProtocol:  7,
			HasDaemonProtocol: false,
			TUIVersion:        "1.2.3",
			DaemonVersion:     "1.2.2",
		},
	}

	effects := Reduce(&model, ExchangeDiagnosticsCopyRequested{})
	if len(effects) != 1 {
		t.Fatalf("effects len=%d want 1", len(effects))
	}
	copyEff, ok := effects[0].(stateeffect.CopyExchangeDiagnosticsEffect)
	if !ok {
		t.Fatalf("effect type=%T want CopyExchangeDiagnosticsEffect", effects[0])
	}
	if strings.Contains(copyEff.Text, "exchange stages:") {
		t.Fatalf("copy text unexpectedly contains stage section: %q", copyEff.Text)
	}
	if !strings.Contains(copyEff.Text, "protocol mismatch: expected 7, got missing") {
		t.Fatalf("copy text missing mismatch line: %q", copyEff.Text)
	}
}
