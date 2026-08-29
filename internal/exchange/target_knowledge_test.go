package exchange

import (
	"testing"

	"github.com/swobuforge/swobu/internal/provider"
)

func TestTargetExceptionsAreScopedByWorkspaceAndTargetGeneration(t *testing.T) {
	knowledge := newTargetExceptions()
	base := targetExceptionGeneration{workspace: "alpha", targetID: "target", targetVersion: 1}
	knowledge.observe(base, provider.AcceptsParallelToolCallsFalse, false)

	if value, ok := knowledge.lookup(base, provider.AcceptsParallelToolCallsFalse); !ok || value {
		t.Fatalf("base lookup = %t, %t", value, ok)
	}
	for _, other := range []targetExceptionGeneration{
		{workspace: "beta", targetID: "target", targetVersion: 1},
		{workspace: "alpha", targetID: "other", targetVersion: 1},
		{workspace: "alpha", targetID: "target", targetVersion: 2},
	} {
		if _, ok := knowledge.lookup(other, provider.AcceptsParallelToolCallsFalse); ok {
			t.Fatalf("knowledge leaked to generation %#v", other)
		}
	}
}

func TestTargetExceptionsStoreOnlyRejectedPreferredWire(t *testing.T) {
	knowledge := newTargetExceptions()
	generation := targetExceptionGeneration{workspace: "alpha", targetID: "target", targetVersion: 1}
	knowledge.observe(generation, provider.AcceptsParallelToolCallsFalse, false)
	if value, ok := knowledge.lookup(generation, provider.AcceptsParallelToolCallsFalse); !ok || value {
		t.Fatalf("preferred rejection = %t, %t", value, ok)
	}
	knowledge.observe(generation, provider.AcceptsParallelToolCallsFalse, true)
	if _, ok := knowledge.lookup(generation, provider.AcceptsParallelToolCallsFalse); ok {
		t.Fatal("authoritative preferred acceptance did not remove exception")
	}
}

func TestCloneFactReadsFreezesAttemptEvidence(t *testing.T) {
	source := map[provider.TargetFact]bool{provider.AcceptsReasoningEffortMax: false}
	frozen := cloneFactReads(source)
	source[provider.AcceptsReasoningEffortMax] = true
	if frozen[provider.AcceptsReasoningEffortMax] {
		t.Fatal("attempt fact reads changed after source mutation")
	}
}
