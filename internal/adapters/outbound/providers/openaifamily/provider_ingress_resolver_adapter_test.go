package openaifamily

import (
	"context"
	"errors"
	"net/http"
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
