package canonical

import (
	"net/http"
	"strings"
	"testing"
)

func TestNewSwobuError_UsesCanonicalOriginAndCode(t *testing.T) {
	err := UnsupportedEndpoint("unsupported normalized path")

	if err.Origin != ErrorOriginSwobu {
		t.Fatalf("origin = %q, want %q", err.Origin, ErrorOriginSwobu)
	}
	if err.Code != ErrorCodeUnsupportedEndpoint {
		t.Fatalf("code = %q, want %q", err.Code, ErrorCodeUnsupportedEndpoint)
	}
}

func TestNewBackendError_PreservesBackendOriginAndRetryAfter(t *testing.T) {
	err := NewBackendError("backend-a", http.StatusTooManyRequests, "rate limited", "120")

	if err.Origin != ErrorOriginBackend {
		t.Fatalf("origin = %q, want %q", err.Origin, ErrorOriginBackend)
	}
	if err.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status code = %d, want %d", err.StatusCode, http.StatusTooManyRequests)
	}
	if err.RetryAfterHeaderValue != "120" {
		t.Fatalf("retry_after = %q, want %q", err.RetryAfterHeaderValue, "120")
	}
}

func TestIsBackendErrorClass_MatchesWrappedClassification(t *testing.T) {
	base := NewBackendError("backend-a", http.StatusBadRequest, "bad tool choice", "")
	err := NewClassifiedBackendError(BackendErrorClassToolChoiceUnsupported, base)
	if !IsBackendErrorClass(err, BackendErrorClassToolChoiceUnsupported) {
		t.Fatal("expected capability classification to match")
	}
}

func TestRecoveryOwnedSwobuErrors_AreCanonicalAndActionable(t *testing.T) {
	cases := []struct {
		name string
		err  Error
		code ErrorCode
	}{
		{
			name: "swobu capability gap",
			err:  NotImplemented("Responses input item type tool_search has no canonical projection"),
			code: ErrorCodeNotImplemented,
		},
		{
			name: "configured targets temporarily unavailable",
			err:  NoAvailableTarget("no currently available configured target can serve the request"),
			code: ErrorCodeNoAvailableTarget,
		},
		{
			name: "provider timeout",
			err:  ProviderTimeout("provider did not respond before the configured deadline"),
			code: ErrorCodeProviderTimeout,
		},
		{
			name: "caller controlled operation",
			err: ClientUnsupportedOperation(
				"models endpoint does not support POST",
				"Change the HTTP method to GET and retry",
			),
			code: ErrorCodeUnsupportedOperation,
		},
		{
			name: "caller controlled delivery",
			err: ClientUnsupportedDelivery(
				"Messages does not support message-oriented delivery",
				"Use buffered or SSE HTTP delivery and retry",
			),
			code: ErrorCodeUnsupportedDelivery,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.err.Code != tc.code {
				t.Fatalf("code = %q, want %q", tc.err.Code, tc.code)
			}
			if tc.err.Origin != ErrorOriginSwobu {
				t.Fatalf("origin = %q, want %q", tc.err.Origin, ErrorOriginSwobu)
			}
			if !ValidErrorCode(tc.err.Code) {
				t.Fatalf("code %q is not a valid canonical error code", tc.err.Code)
			}
		})
	}

	actionable := ClientUnsupportedOperation(
		"models endpoint does not support POST",
		"Change the HTTP method to GET and retry",
	)
	if !strings.Contains(actionable.Message, "Change the HTTP method to GET and retry") {
		t.Fatalf("message = %q, want concrete retry change", actionable.Message)
	}
}
