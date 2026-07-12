package replay

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/adapters/wire/exchangeruntime"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/exchange"
)

func TestExchangeReplay_ResponsesStreamErrorPath(t *testing.T) {
	t.Parallel()

	root := repoRootFromHere(t)
	caseDir := filepath.Join(root, "testdata", "exchange", "upstream_stream_to_client_buffered")
	contract := mustLoadCaseContract(t, filepath.Join(caseDir, "case.yaml"))
	clientFamily, providerFamily := mustMapFamilies(t, contract)
	clientDelivery, providerDelivery := mustMapDeliveries(t, contract)
	if clientDelivery.Mode != delivery.Buffered || providerDelivery.Mode != delivery.Streaming {
		t.Fatalf("unexpected deliveries client=%s provider=%s", clientDelivery, providerDelivery)
	}

	resolver := exchangeruntime.NewResolver()
	clientCodec := resolver.ClientCodec(clientFamily)
	if clientCodec == nil {
		t.Fatalf("client codec missing for family %s", clientFamily)
	}

	clientRequest := []byte(readFile(t, filepath.Join(caseDir, "client_request.body.json")))
	result, err := clientCodec.DecodeClientRequest(carrier.NewWireDocument(
		carrier.StageClientRequestIn,
		protocolkind.ProtocolKind(clientFamily),
		"application/json",
		nil,
		clientRequest,
		carrier.Meta{},
	))
	if err != nil {
		t.Fatalf("decode client request: %v", err)
	}
	request := result.Value.Request

	malformedUpstream := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","model":"m","status":"in_progress"}}`,
		``,
		`data: {"type":"error","error":{"message":"boom"}}`,
		``,
	}, "\n")

	runner := withRuntimeRunner(func(_ context.Context, req exchange.ProviderRequest) (exchange.ProviderIngress, error) {
		return carrier.WireStream{
			Stage:   carrier.StageProviderIngressIn,
			Family:  req.Target.ProtocolKind,
			Framing: carrier.FramingSSE,
			Frames:  carrier.FrameReaderFromReadCloser(io.NopCloser(strings.NewReader(malformedUpstream))),
		}, nil
	})

	_, err = runner.Run(context.Background(), exchange.ExchangeInput{
		ExchangeID:       "fixture_exchange_error",
		ClientFamily:     clientFamily,
		ClientDelivery:   clientDelivery,
		Request:          request,
		ProviderProtocol: providerFamily,
		ProviderDelivery: providerDelivery,
		Target:           exchange.NewRoutableTarget("openai", "openai", "https://example.test/v1", "cred-1", providerFamily, "", "", ""),
		Contract:         exchange.NewExecutionContract(clientDelivery).WithProviderDelivery(providerDelivery),
	})
	if err == nil {
		t.Fatal("expected runner to fail on malformed provider error frame")
	}

	var compatErr canonical.Error
	if !errors.As(err, &compatErr) {
		t.Fatalf("expected canonical error, got %T: %v", err, err)
	}
	if compatErr.Code != canonical.ErrorCodeInternal {
		t.Fatalf("error code = %q, want %q", compatErr.Code, canonical.ErrorCodeInternal)
	}
	if !strings.Contains(err.Error(), "responses stream returned an error event") {
		t.Fatalf("error = %q, want responses stream error", err.Error())
	}
}
