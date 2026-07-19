package responses

import (
	"errors"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

func TestDecodeClientRequest_RejectsExplicitStore(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"true", "false"} {
		value := value
		t.Run(value, func(t *testing.T) {
			t.Parallel()

			_, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.Document{
				Family: protocolkind.Responses,
				Media:  "application/json",
				Raw:    []byte(`{"model":"m","input":"hi","store":` + value + `}`),
			})
			var canonicalErr canonical.Error
			if !errors.As(err, &canonicalErr) {
				t.Fatalf("error = %v, want canonical error", err)
			}
			if canonicalErr.Code != canonical.ErrorCodeUnsupportedOperation {
				t.Fatalf("error code = %q, want %q", canonicalErr.Code, canonical.ErrorCodeUnsupportedOperation)
			}
		})
	}
}
