package exchange

import (
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	trafficevidence "github.com/swobuforge/swobu/internal/domain/trafficevidence"
)

func TestReusablePrefixEvidenceProjectsEphemeralComparison(t *testing.T) {
	occurrence := canonical.ToolIndexOccurrence(2)
	got := reusablePrefixEvidence(canonical.ReusablePrefixComparison{ToolChanged: &occurrence}, "exchange")
	if got.State() != trafficevidence.ReusablePrefixChanged {
		t.Fatalf("state = %q", got.State())
	}
	if kind, ok := got.ChangeKind(); !ok || kind != trafficevidence.ReusablePrefixTool {
		t.Fatalf("kind = %q, %t", kind, ok)
	}
}

func TestTerminalReusablePrefixReflectsWinningRepresentation(t *testing.T) {
	ordinary := exchangeState{reusablePrefix: trafficevidence.PreservedReusablePrefix()}
	if got := terminalReusablePrefix(ordinary); got.State() != trafficevidence.ReusablePrefixPreserved {
		t.Fatalf("ordinary state = %q", got.State())
	}
	native := ordinary
	native.providerCallAttempts = []providerCallAttempt{{nativePreviousResponse: true}}
	if got := terminalReusablePrefix(native); got.State() != trafficevidence.ReusablePrefixNative {
		t.Fatalf("native state = %q", got.State())
	}
	fullHistoryWinner := native
	fullHistoryWinner.providerCallAttempts = append(fullHistoryWinner.providerCallAttempts, providerCallAttempt{nativePreviousResponse: false})
	if got := terminalReusablePrefix(fullHistoryWinner); got.State() != trafficevidence.ReusablePrefixPreserved {
		t.Fatalf("full-history winner state = %q", got.State())
	}
}

func TestTerminalReusablePrefixWithoutPredecessorIsUnknown(t *testing.T) {
	if got := terminalReusablePrefix(exchangeState{}); got.State() != trafficevidence.ReusablePrefixUnknown {
		t.Fatalf("new-session evidence = %#v", got)
	}
}
