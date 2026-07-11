package core

import (
	"bytes"
	"io"
	"net/http"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

func TestCarrierWireDocumentFromWireDocument_PreservesHeadersAndRaw(t *testing.T) {
	t.Parallel()

	h := http.Header{"X-Test": {"a"}}
	raw := []byte(`{"k":"v"}`)
	in := WireDocument{
		Kind:     WireKindRequest,
		Protocol: protocolkind.Responses,
		Method:   http.MethodPost,
		Path:     "/v1/responses",
		Headers:  h,
		RawBody:  raw,
	}

	out := CarrierWireDocumentFromWireDocument(in, carrier.LegClientRequestIn, carrier.Meta{})
	if out.Leg != carrier.LegClientRequestIn {
		t.Fatalf("leg=%q", out.Leg)
	}
	if out.Family != protocolkind.Responses {
		t.Fatalf("family=%q", out.Family)
	}
	if out.Header.Get("X-Test") != "a" {
		t.Fatalf("header not preserved: %#v", out.Header)
	}
	if !bytes.Equal(out.Raw, raw) {
		t.Fatalf("raw mismatch: %q", string(out.Raw))
	}

	raw[0] = 'X'
	if out.Raw[0] == 'X' {
		t.Fatal("raw body must be copied")
	}
}

func TestWireDocumentFromCarrierWireDocument_PreservesHeadersAndRaw(t *testing.T) {
	t.Parallel()

	h := http.Header{"X-Test": {"b"}}
	raw := []byte(`{"ok":true}`)
	in := carrier.WireDocument{
		Leg:    carrier.LegProviderRequestOut,
		Family: protocolkind.ChatCompletions,
		Header: h,
		Raw:    raw,
	}

	out := WireDocumentFromCarrierWireDocument(in)
	if out.Protocol != protocolkind.ChatCompletions {
		t.Fatalf("protocol=%q", out.Protocol)
	}
	if out.Headers.Get("X-Test") != "b" {
		t.Fatalf("header not preserved: %#v", out.Headers)
	}
	if !bytes.Equal(out.RawBody, raw) {
		t.Fatalf("raw mismatch: %q", string(out.RawBody))
	}

	raw[0] = 'X'
	if out.RawBody[0] == 'X' {
		t.Fatal("raw body must be copied")
	}
}

func TestCarrierWireStreamFromWireStream_PreservesFramingAndHeaders(t *testing.T) {
	t.Parallel()

	h := http.Header{"Content-Type": {"text/event-stream"}}
	in := WireStream{
		Kind:     WireKindResponseStream,
		Protocol: protocolkind.Responses,
		Headers:  h,
		Body:     io.NopCloser(bytes.NewBufferString("event: x\n\n")),
		Framing:  FramingSSE,
	}

	mid := CarrierWireStreamFromWireStream(in, carrier.LegProviderResponseIn, carrier.Meta{})
	if mid.Framing != carrier.FramingSSE {
		t.Fatalf("framing=%q", mid.Framing)
	}
	if mid.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("header=%#v", mid.Header)
	}

}
