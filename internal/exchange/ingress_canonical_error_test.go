package exchange

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/trafficevidence"
)

// terminalCanonicalErrorCode carries a code iff the terminal error is (or wraps)
// a canonical Swobu error; everything else — backend failures, plain errors, nil
// — folds to "". This is the raw-fact invariant the product report relies on; the
// analytical failure taxonomy is derived downstream, never invented here. See
// product-telemetry.md.
func TestTerminalCanonicalErrorCode_PresentIffCanonical(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want canonical.ErrorCode
	}{
		{name: "nil", err: nil, want: ""},
		{name: "canonical direct", err: canonical.BadRequest("nope"), want: "BAD_REQUEST"},
		{name: "canonical wrapped", err: fmt.Errorf("delivery failed: %w", canonical.UnknownTarget("gone")), want: "UNKNOWN_TARGET"},
		{name: "backend error is not canonical", err: canonical.NewBackendError("t", 502, "boom", ""), want: ""},
		{name: "plain error", err: errors.New("anything"), want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := terminalCanonicalErrorCode(tc.err); got != tc.want {
				t.Fatalf("terminalCanonicalErrorCode(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}

func TestRequestOutcomeFromSwobuError_UsesRecoveryOwnedStatusAndResult(t *testing.T) {
	cases := []struct {
		name       string
		code       canonical.ErrorCode
		wantResult trafficevidence.ResultClass
		wantStatus int
	}{
		{
			name:       "Swobu capability",
			code:       canonical.ErrorCodeNotImplemented,
			wantResult: trafficevidence.ResultClassNotImplemented,
			wantStatus: http.StatusNotImplemented,
		},
		{
			name:       "target configuration",
			code:       canonical.ErrorCodeNoCompatibleTarget,
			wantResult: trafficevidence.ResultClassNoCompatibleTarget,
			wantStatus: http.StatusBadGateway,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := requestOutcomeFromSwobuError(tc.code); got != tc.wantResult {
				t.Fatalf("result = %q, want %q", got, tc.wantResult)
			}
			if got := requestOutcomeStatusForSwobuError(tc.code); got != tc.wantStatus {
				t.Fatalf("status = %d, want %d", got, tc.wantStatus)
			}
		})
	}
}
