package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestStatusCodeForSwobuError_UsesRecoveryOwnership(t *testing.T) {
	cases := []struct {
		name string
		code canonical.ErrorCode
		want int
	}{
		{name: "bad request", code: canonical.ErrorCodeBadRequest, want: http.StatusBadRequest},
		{name: "client operation", code: canonical.ErrorCodeUnsupportedOperation, want: http.StatusBadRequest},
		{name: "Swobu capability", code: canonical.ErrorCodeNotImplemented, want: http.StatusNotImplemented},
		{name: "target configuration", code: canonical.ErrorCodeNoCompatibleTarget, want: http.StatusBadGateway},
		{name: "temporary target availability", code: canonical.ErrorCodeNoAvailableTarget, want: http.StatusServiceUnavailable},
		{name: "Swobu invariant", code: canonical.ErrorCodeInternal, want: http.StatusInternalServerError},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := statusCodeForSwobuError(tc.code); got != tc.want {
				t.Fatalf("status = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestWriteSwobuError_EmitsRecoveryOwnedHTTPStatusAndCode(t *testing.T) {
	cases := []struct {
		name string
		err  canonical.Error
		want int
	}{
		{
			name: "Swobu capability gap",
			err:  canonical.NotImplemented("Swobu cannot project this valid protocol item"),
			want: http.StatusNotImplemented,
		},
		{
			name: "configured targets exhausted",
			err:  canonical.NoCompatibleTarget("no configured target can represent the canonical request"),
			want: http.StatusBadGateway,
		},
		{
			name: "configured targets temporarily unavailable",
			err:  canonical.NoAvailableTarget("no currently available configured target can serve the request"),
			want: http.StatusServiceUnavailable,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			writeSwobuError(response, tc.err)
			if response.Code != tc.want {
				t.Fatalf("status = %d, want %d", response.Code, tc.want)
			}
			var envelope swobuErrorEnvelope
			if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			if envelope.Error.Code != string(tc.err.Code) {
				t.Fatalf("code = %q, want %q", envelope.Error.Code, tc.err.Code)
			}
		})
	}
}
