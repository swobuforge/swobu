package exchange

import (
	"errors"
	"testing"

	"github.com/swobuforge/swobu/internal/provider"
)

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
		wantDuplicateRisk     bool
	}{
		{name: "empty", completed: true},
		{name: "single call", attempts: []providerCallAttempt{{candidateIndex: 0}}, candidateCount: 1, completed: true, wantProviderCalls: 1},
		{
			name: "same candidate uncertain retry is not fallback",
			attempts: []providerCallAttempt{
				{
					candidateIndex: 0,
					status:         providerCallAttemptFailed,
					failure: &providerCallFailure{
						Attempt: provider.AttemptMayHaveExecuted(provider.Unavailable(errors.New("uncertain"))),
					},
				},
				{candidateIndex: 0},
			},
			candidateCount: 1, completed: true, wantProviderCalls: 2, wantDuplicateRisk: true,
		},
		{name: "codec rejection then one call recovered", attempts: []providerCallAttempt{{candidateIndex: 1}}, candidateCount: 2, completed: true, wantProviderCalls: 1, wantFallbackRecovered: true},
		{name: "fallback attempted but not recovered", attempts: []providerCallAttempt{{candidateIndex: 1}}, candidateCount: 2, completed: false, wantProviderCalls: 1},
		{name: "three candidates recovered", attempts: []providerCallAttempt{{candidateIndex: 2}}, candidateCount: 3, completed: true, wantProviderCalls: 1, wantFallbackRecovered: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			summary := summarizeRoutingEvidence(tc.attempts, tc.candidateCount, tc.completed)
			if summary.providerCallCount != tc.wantProviderCalls ||
				summary.fallbackRecovered != tc.wantFallbackRecovered ||
				summary.possibleDuplicateExecution != tc.wantDuplicateRisk {
				t.Fatalf("summarizeRoutingEvidence = %#v, want calls=%d recovered=%t duplicate=%t",
					summary, tc.wantProviderCalls, tc.wantFallbackRecovered, tc.wantDuplicateRisk)
			}
		})
	}
}
