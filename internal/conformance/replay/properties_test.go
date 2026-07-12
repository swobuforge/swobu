package replay

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/exchange"
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

	runner := withRuntimeRunner(func(_ context.Context, _ exchange.ProviderRequest) (exchange.ProviderIngress, error) {
		return carrier.WireStream{
			Stage:   carrier.StageProviderIngressIn,
			Family:  protocolkind.Responses,
			Framing: carrier.FramingSSE,
			Frames:  carrier.FrameReaderFromReadCloser(providerRead),
		}, nil
	})
	out, err := runner.Run(context.Background(), exchange.ExchangeInput{
		ExchangeID:       "lazy_stream_client",
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.StreamingDelivery(delivery.FramingSSE),
		Request:          testCanonicalRequest("m"),
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.StreamingDelivery(delivery.FramingSSE),
		Target:           exchange.NewRoutableTarget("openai", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "", "responses_stream"),
		Contract:         exchange.NewExecutionContract(delivery.StreamingDelivery(delivery.FramingSSE)),
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if out.Transport.Body == nil {
		t.Fatal("expected streaming client output")
	}

	readDone := make(chan string, 1)
	readErr := make(chan error, 1)
	streamBody := out.Transport.Body
	defer func() { _ = streamBody.Close() }()
	go func() {
		buf := make([]byte, 512)
		n, err := streamBody.Read(buf)
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
	var out exchange.TransportResponse
	var runErr error
	go func() {
		defer close(done)
		runner := withRuntimeRunner(func(_ context.Context, _ exchange.ProviderRequest) (exchange.ProviderIngress, error) {
			return carrier.CanonicalEventStream{Events: blocking}, nil
		})
		out, runErr = runner.Run(context.Background(), exchange.ExchangeInput{
			ExchangeID:       "buffered_collection_boundary",
			ClientFamily:     canonical.ClientFamilyResponses,
			ClientDelivery:   delivery.BufferedDelivery(),
			Request:          testCanonicalRequest("m"),
			ProviderProtocol: protocolkind.Responses,
			ProviderDelivery: delivery.BufferedDelivery(),
			Target:           exchange.NewRoutableTarget("openai", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "", "responses"),
			Contract:         exchange.NewExecutionContract(delivery.BufferedDelivery()),
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
	if out.Transport.Body == nil {
		t.Fatal("buffered output missing body")
	}
	raw, err := io.ReadAll(out.Transport.Body)
	if err != nil {
		t.Fatalf("read buffered output: %v", err)
	}
	if strings.TrimSpace(string(raw)) == "" { // swobu:io-string source=domain
		t.Fatal("buffered output body is empty")
	}
	if !strings.Contains(string(raw), "\"status\":\"completed\"") {
		t.Fatalf("buffered output missing completion status: %s", string(raw))
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

func testCanonicalRequest(model string) canonical.CanonicalRequest {
	return canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: model,
		Items: []canonical.CanonicalItem{
			canonical.NewTextItem(canonical.ItemAuthorUser, "hi"),
		},
	})
}
