package exchange

import (
	"testing"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestNativeContinuationUsesReplayStructureNotCompatibilityEvidence(t *testing.T) {
	t.Run("exact structural mutation suppresses native continuation", func(t *testing.T) {
		replayChanged := true
		changes := []compat.Change(nil)
		if len(changes) != 0 {
			t.Fatalf("exact replay transform changes = %#v, want none", changes)
		}
		if providerHistoryHandleEligible(providerRequestPreferred, replayChanged) {
			t.Fatal("native continuation remained eligible after structural replay mutation")
		}
	})

	t.Run("non-history semantic loss does not suppress native continuation", func(t *testing.T) {
		replayChanged := false
		changes := []compat.Change{compat.NewOmission(canonical.RequestReasoning, canonical.Occurrence{})}
		if len(changes) == 0 {
			t.Fatal("fixture must carry semantic-loss evidence")
		}
		if !providerHistoryHandleEligible(providerRequestPreferred, replayChanged) {
			t.Fatal("semantic-loss evidence suppressed structurally exact native continuation")
		}
	})
}
