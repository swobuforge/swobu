package responses

import (
	"errors"
	"reflect"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

func TestDecodeClientRequest_StorageIntentDoesNotChangeCanonicalRequest(t *testing.T) {
	t.Parallel()

	decoder := ClientRequestDecoder{}
	omitted, err := decoder.DecodeClientRequest(carrier.Document{
		Family: protocolkind.Responses,
		Media:  "application/json",
		Raw:    []byte(`{"model":"m","input":"hi"}`),
	})
	if err != nil {
		t.Fatalf("decode omitted store: %v", err)
	}
	for _, value := range []string{"false", "true", "null"} {
		t.Run(value, func(t *testing.T) {
			t.Parallel()

			got, err := decoder.DecodeClientRequest(carrier.Document{
				Family: protocolkind.Responses,
				Media:  "application/json",
				Raw:    []byte(`{"model":"m","input":"hi","store":` + value + `}`),
			})
			if err != nil {
				t.Fatalf("decode store=%s: %v", value, err)
			}
			if !reflect.DeepEqual(got, omitted) {
				t.Fatalf("store=%s changed canonical decode\n got: %#v\nwant: %#v", value, got, omitted)
			}
		})
	}
}

func TestDecodeClientRequest_RejectsMalformedStoreValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
	}{
		{name: "string", value: `"false"`},
		{name: "object", value: `{}`},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.Document{
				Family: protocolkind.Responses,
				Media:  "application/json",
				Raw:    []byte(`{"model":"m","input":"hi","store":` + test.value + `}`),
			})
			var canonicalErr canonical.Error
			if !errors.As(err, &canonicalErr) {
				t.Fatalf("error = %v, want canonical error", err)
			}
			if canonicalErr.Code != canonical.ErrorCodeBadRequest {
				t.Fatalf("error code = %q, want %q", canonicalErr.Code, canonical.ErrorCodeBadRequest)
			}
		})
	}
}
