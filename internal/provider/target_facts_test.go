package provider

import "testing"

func TestTargetFactsUnknownUsesPreferredAndRecordsEveryRead(t *testing.T) {
	facts := NewTargetFacts(func(fact TargetFact) (bool, bool) {
		if fact == AcceptsReasoningDisabled {
			return false, true
		}
		return false, false
	})
	if !facts.AcceptsParallelToolCallsFalse() {
		t.Fatal("unknown fact did not use preferred true")
	}
	if facts.AcceptsReasoningDisabled() {
		t.Fatal("known false fact did not use fallback branch")
	}
	reads := facts.Reads()
	if len(reads) != 2 || !reads[AcceptsParallelToolCallsFalse] || reads[AcceptsReasoningDisabled] {
		t.Fatalf("reads = %#v", reads)
	}
	reads[AcceptsReasoningDisabled] = true
	if facts.AcceptsReasoningDisabled() {
		t.Fatal("detached read snapshot mutated attempt facts")
	}
}
