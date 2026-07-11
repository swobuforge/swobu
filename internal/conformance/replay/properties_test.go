package replay

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/ports"
)

func TestStreamingClientOutputIsLazy(t *testing.T) {
	t.Parallel()

	providerRead, providerWrite := io.Pipe()
	release := make(chan struct{})
	go func() {
		defer func() { _ = providerWrite.Close() }()
		_, _ = io.WriteString(providerWrite, "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"status\":\"in_progress\",\"output\":[]}}\n\n")
		<-release
		_, _ = io.WriteString(providerWrite, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"status\":\"completed\",\"output\":[]}}\n\n")
	}()

	out, err := (exchange.Runner{}).Run(context.Background(), exchange.ClientInput{
		ExchangeID:       "lazy_stream_client",
		ClientFamily:     canonical.IngressFamilyResponses,
		ClientDelivery:   delivery.StreamingDelivery(delivery.FramingSSE),
		ClientRequestRaw: []byte(`{"model":"m","input":"hi","stream":true}`),
		ProviderFamily:   protocolkind.Responses,
		ProviderDelivery: delivery.StreamingDelivery(delivery.FramingSSE),
		Target:           ports.NewRoutableTarget("openai", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "", "responses_stream"),
		Contract:         ports.NewExecutionContract(delivery.StreamingDelivery(delivery.FramingSSE)),
		ProviderExecute: func(context.Context, ports.ProviderRequest) (ports.ProviderTransportResponse, error) {
			return ports.ProviderTransportResponse{Stream: providerRead}, nil
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if out.Stream == nil {
		t.Fatal("expected streaming client output")
	}

	readDone := make(chan string, 1)
	readErr := make(chan error, 1)
	go func() {
		buf := make([]byte, 512)
		n, err := out.Stream.Body.Read(buf)
		if err != nil && err != io.EOF {
			readErr <- err
			return
		}
		readDone <- string(buf[:n])
	}()

	select {
	case err := <-readErr:
		t.Fatalf("read first frame error: %v", err)
	case first := <-readDone:
		if !strings.Contains(first, "response.created") {
			t.Fatalf("first frame missing response.created: %s", first)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for first streamed frame")
	}

	close(release)
}

func TestBufferedProjectionIsCollectionBoundary(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	envelope := canonical.NewSliceEventReader(canonical.EventSequence{
		{ExchangeID: "ex", Seq: 1, Kind: canonical.EventEnvelopeStart, EnvID: "resp_1", Payload: canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse}},
		{ExchangeID: "ex", Seq: 2, Kind: canonical.EventTextDelta, EnvID: "resp_1", Payload: canonical.TextDeltaPayload{Text: "ok"}},
		{ExchangeID: "ex", Seq: 3, Kind: canonical.EventEnvelopeEnd, EnvID: "resp_1", Payload: canonical.EnvelopeEndPayload{Kind: canonical.EnvResponse, Status: canonical.EnvelopeStatusCompleted}},
	})
	blocking := &blockingReader{inner: envelope, release: release}

	done := make(chan struct{})
	var out exchange.ClientOutput
	var runErr error
	go func() {
		defer close(done)
		out, runErr = (exchange.Runner{}).Run(context.Background(), exchange.ClientInput{
			ExchangeID:       "buffered_collection_boundary",
			ClientFamily:     canonical.IngressFamilyResponses,
			ClientDelivery:   delivery.BufferedDelivery(),
			ClientRequestRaw: []byte(`{"model":"m","input":"hi"}`),
			ProviderFamily:   protocolkind.Responses,
			ProviderDelivery: delivery.BufferedDelivery(),
			Target:           ports.NewRoutableTarget("openai", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "", "responses"),
			Contract:         ports.NewExecutionContract(delivery.BufferedDelivery()),
			ProviderExecute: func(context.Context, ports.ProviderRequest) (ports.ProviderTransportResponse, error) {
				return ports.ProviderTransportResponse{Envelope: blocking}, nil
			},
		})
	}()

	select {
	case <-done:
		t.Fatal("buffered projection returned before terminal envelope release")
	case <-time.After(150 * time.Millisecond):
	}

	close(release)
	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("buffered projection did not complete after release")
	}

	if runErr != nil {
		t.Fatalf("Run() error = %v", runErr)
	}
	if out.Document == nil {
		t.Fatal("buffered output missing document")
	}
}

type blockingReader struct {
	inner   canonical.EventReader
	release <-chan struct{}
	count   int
}

func (r *blockingReader) Next(ctx context.Context) (canonical.Event, error) {
	r.count++
	if r.count > 1 {
		<-r.release
	}
	return r.inner.Next(ctx)
}

func (r *blockingReader) Close(ctx context.Context) error { return r.inner.Close(ctx) }
