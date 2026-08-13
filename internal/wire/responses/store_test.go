package responses

import (
	"errors"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

func TestResponsesStorePreservesOccurrenceThroughDecodeAndEncode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		field         string
		wantValue     bool
		wantSpecified bool
		wantEncoded   string
	}{
		{name: "omitted"},
		{name: "null", field: `,"store":null`},
		{name: "false", field: `,"store":false`, wantSpecified: true, wantEncoded: `"store":false`},
		{name: "true", field: `,"store":true`, wantValue: true, wantSpecified: true, wantEncoded: `"store":true`},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.Document{
				Family: protocolkind.Responses,
				Media:  "application/json",
				Raw:    []byte(`{"model":"m","input":"hi"` + test.field + `}`),
			})
			if err != nil {
				t.Fatalf("decode store %s: %v", test.name, err)
			}
			gotValue, gotSpecified := decoded.Request.Request.Store()
			if gotValue != test.wantValue || gotSpecified != test.wantSpecified {
				t.Fatalf("decoded store = (%t,%t), want (%t,%t)", gotValue, gotSpecified, test.wantValue, test.wantSpecified)
			}
			document, err := EncodeCarrierWithChanges(
				EncodeInput{Request: decoded.Request.Request, ToolNames: testAttemptToolNames(decoded.Request.Request)},
				delivery.BufferedDelivery(),
				nil,
				"",
				EncodeOptions{},
			)
			if err != nil {
				t.Fatalf("encode store %s: %v", test.name, err)
			}
			raw := string(document.RawBytes())
			if test.wantEncoded == "" && strings.Contains(raw, `"store"`) {
				t.Fatalf("unspecified store encoded: %s", raw)
			}
			if test.wantEncoded != "" && !strings.Contains(raw, test.wantEncoded) {
				t.Fatalf("encoded store %s = %s", test.name, raw)
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
