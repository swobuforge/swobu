package canonical

import (
	"net/http"
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
