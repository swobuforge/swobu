package exchange

import "testing"

// summarizeRoutingEvidence keeps evaluated candidates distinct from provider
// calls. Codec-local rejection can advance a candidate without issuing I/O.
func TestSummarizeRoutingEvidence(t *testing.T) {
	cases := []struct {
		name                  string
		attempts              []providerCallAttempt
		candidateCount        int
		completed             bool
		wantProviderCalls     int
		wantFallbackRecovered bool
	}{
		{name: "empty", completed: true},
		{name: "single call", attempts: []providerCallAttempt{{candidateIndex: 0}}, candidateCount: 1, completed: true, wantProviderCalls: 1},
		{name: "same candidate retry is not fallback", attempts: []providerCallAttempt{{candidateIndex: 0}, {candidateIndex: 0}}, candidateCount: 1, completed: true, wantProviderCalls: 2},
		{name: "codec rejection then one call recovered", attempts: []providerCallAttempt{{candidateIndex: 1}}, candidateCount: 2, completed: true, wantProviderCalls: 1, wantFallbackRecovered: true},
		{name: "fallback attempted but not recovered", attempts: []providerCallAttempt{{candidateIndex: 1}}, candidateCount: 2, completed: false, wantProviderCalls: 1},
		{name: "three candidates recovered", attempts: []providerCallAttempt{{candidateIndex: 2}}, candidateCount: 3, completed: true, wantProviderCalls: 1, wantFallbackRecovered: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			summary := summarizeRoutingEvidence(tc.attempts, tc.candidateCount, tc.completed)
			if summary.providerCallCount != tc.wantProviderCalls ||
				summary.fallbackRecovered != tc.wantFallbackRecovered {
				t.Fatalf("summarizeRoutingEvidence = %#v, want calls=%d recovered=%t",
					summary, tc.wantProviderCalls, tc.wantFallbackRecovered)
			}
		})
	}
}
