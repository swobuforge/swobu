package openaifamily

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/profile"
)

type failingRoundTripper struct {
	err error
}

type closeTrackingBody struct {
	io.Reader
	closed bool
}

func (b *closeTrackingBody) Close() error { b.closed = true; return nil }

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func (t failingRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, t.err
}

func TestResolveProviderIngress_PreservesTransportErrorDetail(t *testing.T) {
	t.Parallel()

	transportErr := errors.New("dial tcp 127.0.0.1:11434: connect: connection refused")
	exec := NewExecutor(&http.Client{Transport: failingRoundTripper{err: transportErr}}, nil, NewOllamaPolicy())
	req := exchange.ProviderRequest{
		Request: canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: "gpt-4o-mini",
		}),
		RequestDocument: carrier.NewCarrierDocument(
			carrier.StageProviderRequestOut,
			protocolkind.Responses,
			"application/json",
			nil,
			[]byte(`{"model":"gpt-4o-mini","input":"hello"}`),
			carrier.Meta{},
		),
		Contract: exchange.NewExecutionContract(delivery.BufferedDelivery()),
		Target: exchange.NewRoutableTarget(
			"backend-a",
			string(profile.ProviderSpecOllama),
			"http://127.0.0.1:11434/v1",
			"",
			protocolkind.Responses,
			"",
			"",
		),
		ExchangeID:   "ex_transport",
		ClientFamily: canonical.ClientFamilyResponses,
	}

	_, err := exec.ResolveProviderIngress(context.Background(), req)
	if err == nil {
		t.Fatal("expected ResolveProviderIngress to fail")
	}

	var swErr canonical.Error
	if !errors.As(err, &swErr) {
		t.Fatalf("error type = %T, want canonical.Error", err)
	}
	if swErr.Code != canonical.ErrorCodeBadEndpoint {
		t.Fatalf("error code = %s, want %s", swErr.Code, canonical.ErrorCodeBadEndpoint)
	}
	if got := swErr.Details["request_transport_error"]; got != transportErr.Error() {
		t.Fatalf("transport detail = %q, want %q", got, transportErr.Error())
	}
}

func TestResolveProviderIngress_BoundsNonSSEStreamingEvidenceAndClosesBody(t *testing.T) {
	body := &closeTrackingBody{Reader: strings.NewReader(strings.Repeat("x", (64<<10)+4096))}
	client := &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: body, Request: req}, nil
	})}
	exec := NewExecutor(client, nil, NewOllamaPolicy())
	req := exchange.ProviderRequest{
		Request:         canonical.NewCanonicalRequest(canonical.RequestParams{Model: "gpt-4o-mini"}),
		RequestDocument: carrier.NewCarrierDocument(carrier.StageProviderRequestOut, protocolkind.Responses, "application/json", nil, []byte(`{"model":"gpt-4o-mini","input":"hello"}`), carrier.Meta{}),
		Contract:        exchange.NewExecutionContract(delivery.StreamingDelivery(delivery.FramingSSE)),
		Target:          exchange.NewRoutableTarget("backend-a", string(profile.ProviderSpecOllama), "http://127.0.0.1:11434/v1", "", protocolkind.Responses, "", ""),
		ExchangeID:      "ex_non_sse", ClientFamily: canonical.ClientFamilyResponses,
	}
	_, err := exec.ResolveProviderIngress(context.Background(), req)
	var backendErr canonical.BackendError
	if !errors.As(err, &backendErr) || backendErr.StatusCode != http.StatusBadGateway {
		t.Fatalf("error = %#v, want bounded 502 backend error", err)
	}
	if len(backendErr.Message) > 64<<10 {
		t.Fatalf("backend evidence length = %d, want <= %d", len(backendErr.Message), 64<<10)
	}
	if !body.closed {
		t.Fatal("non-SSE response body was not closed")
	}
}
