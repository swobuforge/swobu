package exchange

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	protocolregistry "github.com/swobuforge/swobu/internal/adapters/wire/protocolregistry"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

type blockingEnvelopeReader struct {
	events   []any
	index    int
	releaseC <-chan struct{}
}

func (r *blockingEnvelopeReader) Next(context.Context) (canonical.Event, error) {
	if r.index >= len(r.events) {
		return canonical.Event{}, io.EOF
	}
	if r.index > 0 && r.releaseC != nil {
		<-r.releaseC
	}
	ev, ok := r.events[r.index].(canonical.Event)
	if !ok {
		return canonical.Event{}, io.ErrUnexpectedEOF
	}
	r.index++
	return ev, nil
}

func (r *blockingEnvelopeReader) Close(context.Context) error { return nil }

func TestRunnerRun_BufferedEndToEnd(t *testing.T) {
	runner := Runner{ProviderExecute: bufferedProviderExecutor([]byte(`{"id":"resp_1","model":"m","output_text":"ok"}`))}
	out, err := runner.Run(context.Background(), withRuntime(ClientInput{
		ExchangeID:       "ex_test",
		ClientFamily:     canonical.IngressFamilyResponses,
		ClientDelivery:   delivery.BufferedDelivery(),
		Request:          testCanonicalRequest("m"),
		ProviderFamily:   protocolkind.Responses,
		ProviderDelivery: delivery.BufferedDelivery(),
		Target:           NewRoutableTarget("openai", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "", "responses"),
		Contract:         NewExecutionContract(delivery.BufferedDelivery()),
	}))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if out.Stream != nil {
		t.Fatalf("buffered response must not set stream carrier")
	}
	if out.Document == nil {
		t.Fatalf("buffered response must set document carrier")
	}
	if !strings.Contains(string(out.Document.Raw), "ok") {
		t.Fatalf("client body missing expected text: %s", string(out.Document.Raw))
	}
}

func TestRunnerRun_StreamingEndToEnd(t *testing.T) {
	providerSSE := "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"status\":\"in_progress\",\"output\":[]}}\n\n" +
		"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"response_id\":\"resp_1\",\"item_id\":\"msg_1\",\"delta\":\"ok\"}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"status\":\"completed\",\"output\":[{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"ok\"}]}]}}\n\n"
	runner := Runner{ProviderExecute: streamingProviderExecutor(io.NopCloser(strings.NewReader(providerSSE)))}
	out, err := runner.Run(context.Background(), withRuntime(ClientInput{
		ExchangeID:       "ex_test_stream",
		ClientFamily:     canonical.IngressFamilyResponses,
		ClientDelivery:   delivery.StreamingDelivery(delivery.FramingSSE),
		Request:          testCanonicalRequest("m"),
		ProviderFamily:   protocolkind.Responses,
		ProviderDelivery: delivery.StreamingDelivery(delivery.FramingSSE),
		Target:           NewRoutableTarget("openai", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "", "responses_stream"),
		Contract:         NewExecutionContract(delivery.StreamingDelivery(delivery.FramingSSE)),
	}))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if out.Document != nil {
		t.Fatalf("streaming response must not set document carrier")
	}
	if out.Stream == nil {
		t.Fatalf("client stream carrier was nil")
	}
	streamRaw, readErr := io.ReadAll(carrier.ReadCloserFromFrameReader(out.Stream.Frames))
	if readErr != nil {
		t.Fatalf("read stream: %v", readErr)
	}
	if !strings.Contains(string(streamRaw), "response.completed") {
		t.Fatalf("client stream missing terminal marker: %s", string(streamRaw))
	}
}

func TestStreamingClientDoesNotReadProviderStreamToEOFBeforeFirstFrame(t *testing.T) {
	providerRead, providerWrite := io.Pipe()
	defer func() { _ = providerRead.Close() }()
	release := make(chan struct{})
	go func() {
		defer func() { _ = providerWrite.Close() }()
		_, _ = io.WriteString(providerWrite, "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"status\":\"in_progress\",\"output\":[]}}\n\n")
		<-release
		_, _ = io.WriteString(providerWrite, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"status\":\"completed\",\"output\":[]}}\n\n")
	}()

	runner := Runner{ProviderExecute: streamingProviderExecutor(providerRead)}
	out, err := runner.Run(context.Background(), withRuntime(ClientInput{
		ExchangeID:       "ex_test_stream_non_blocking",
		ClientFamily:     canonical.IngressFamilyResponses,
		ClientDelivery:   delivery.StreamingDelivery(delivery.FramingSSE),
		Request:          testCanonicalRequest("m"),
		ProviderFamily:   protocolkind.Responses,
		ProviderDelivery: delivery.StreamingDelivery(delivery.FramingSSE),
		Target:           NewRoutableTarget("openai", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "", "responses_stream"),
		Contract:         NewExecutionContract(delivery.StreamingDelivery(delivery.FramingSSE)),
	}))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if out.Stream == nil {
		t.Fatalf("client stream carrier was nil")
	}
	readDone := make(chan []byte, 1)
	readErr := make(chan error, 1)
	streamBody := carrier.ReadCloserFromFrameReader(out.Stream.Frames)
	defer func() { _ = streamBody.Close() }()
	go func() {
		buf := make([]byte, 512)
		n, err := streamBody.Read(buf)
		if err != nil && err != io.EOF {
			readErr <- err
			return
		}
		readDone <- buf[:n]
	}()

	select {
	case err := <-readErr:
		t.Fatalf("read first frame error: %v", err)
	case first := <-readDone:
		if !strings.Contains(string(first), "response.created") {
			t.Fatalf("first frame did not contain response.created: %s", string(first))
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for first streamed frame")
	}

	close(release)
}

func TestRunnerRun_RejectsAmbiguousProviderTransportResponse(t *testing.T) {
	runner := Runner{ProviderExecute: func(context.Context, ProviderRequest) (ProviderTransportResponse, error) {
		return ProviderTransportResponse{
			Document: []byte(`{"id":"resp_1","output_text":"ok"}`),
			Stream:   io.NopCloser(strings.NewReader("event: response.completed\ndata: {}\n\n")),
		}, nil
	}}
	_, err := runner.Run(context.Background(), withRuntime(ClientInput{
		ExchangeID:       "ex_invalid_transport_shape",
		ClientFamily:     canonical.IngressFamilyResponses,
		ClientDelivery:   delivery.BufferedDelivery(),
		Request:          testCanonicalRequest("m"),
		ProviderFamily:   protocolkind.Responses,
		ProviderDelivery: delivery.BufferedDelivery(),
		Target:           NewRoutableTarget("openai", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "", "responses"),
		Contract:         NewExecutionContract(delivery.BufferedDelivery()),
	}))
	if err == nil {
		t.Fatal("Run() expected error for ambiguous provider transport response")
	}
	if !strings.Contains(err.Error(), "provider transport response shape is invalid") {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestRunnerRun_RejectsBufferedDeliveryWithTransportStream(t *testing.T) {
	runner := Runner{ProviderExecute: streamingProviderExecutor(io.NopCloser(strings.NewReader("event: response.completed\ndata: {}\n\n")))}
	_, err := runner.Run(context.Background(), withRuntime(ClientInput{
		ExchangeID:       "ex_invalid_delivery_shape",
		ClientFamily:     canonical.IngressFamilyResponses,
		ClientDelivery:   delivery.BufferedDelivery(),
		Request:          testCanonicalRequest("m"),
		ProviderFamily:   protocolkind.Responses,
		ProviderDelivery: delivery.BufferedDelivery(),
		Target:           NewRoutableTarget("openai", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "", "responses"),
		Contract:         NewExecutionContract(delivery.BufferedDelivery()),
	}))
	if err == nil {
		t.Fatal("Run() expected error for buffered delivery with transport stream")
	}
	if !strings.Contains(err.Error(), "provider transport stream requires streaming delivery") {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestStreamingClientChatCompletionsFirstFrameBeforeEnvelopeEOF(t *testing.T) {
	release := make(chan struct{})
	providerEnvelope := &blockingEnvelopeReader{
		releaseC: release,
		events: []any{
			canonical.Event{
				ExchangeID: "ex_chat",
				Seq:        1,
				Kind:       canonical.EventEnvelopeStart,
				EnvID:      "resp_1",
				Payload:    canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse},
			},
			canonical.Event{
				ExchangeID: "ex_chat",
				Seq:        2,
				Kind:       canonical.EventEnvelopeEnd,
				EnvID:      "resp_1",
				Payload:    canonical.EnvelopeEndPayload{Kind: canonical.EnvResponse, Status: canonical.EnvelopeStatusCompleted},
			},
		},
	}
	runner := Runner{ProviderExecute: func(context.Context, ProviderRequest) (ProviderTransportResponse, error) {
		return ProviderTransportResponse{Envelope: providerEnvelope}, nil
	}}
	out, err := runner.Run(context.Background(), withRuntime(ClientInput{
		ExchangeID:       "ex_chat_stream_non_blocking",
		ClientFamily:     canonical.IngressFamilyChatCompletions,
		ClientDelivery:   delivery.StreamingDelivery(delivery.FramingSSE),
		Request:          testCanonicalRequest("m"),
		ProviderFamily:   protocolkind.ChatCompletions,
		ProviderDelivery: delivery.StreamingDelivery(delivery.FramingSSE),
		Target:           NewRoutableTarget("openai", "openai", "https://example.test/v1", "cred-1", protocolkind.ChatCompletions, "", "", "chat_completions_stream"),
		Contract:         NewExecutionContract(delivery.StreamingDelivery(delivery.FramingSSE)),
	}))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if out.Stream == nil {
		t.Fatal("client stream carrier was nil")
	}
	readDone := make(chan []byte, 1)
	readErr := make(chan error, 1)
	streamBody := carrier.ReadCloserFromFrameReader(out.Stream.Frames)
	defer func() { _ = streamBody.Close() }()
	go func() {
		buf := make([]byte, 512)
		n, err := streamBody.Read(buf)
		if err != nil && err != io.EOF {
			readErr <- err
			return
		}
		readDone <- buf[:n]
	}()
	select {
	case err := <-readErr:
		t.Fatalf("read first frame error: %v", err)
	case first := <-readDone:
		if len(first) == 0 {
			t.Fatal("first frame was empty")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for first streamed frame")
	}
	close(release)
}

func bufferedProviderExecutor(raw []byte) func(context.Context, ProviderRequest) (ProviderTransportResponse, error) {
	return func(context.Context, ProviderRequest) (ProviderTransportResponse, error) {
		return ProviderTransportResponse{
			Document: append([]byte(nil), raw...),
		}, nil
	}
}

func streamingProviderExecutor(stream io.ReadCloser) func(context.Context, ProviderRequest) (ProviderTransportResponse, error) {
	return func(context.Context, ProviderRequest) (ProviderTransportResponse, error) {
		return ProviderTransportResponse{Stream: stream}, nil
	}
}

func withRuntime(in ClientInput) ClientInput {
	req, _ := protocolregistry.ForClientRequestDecoder(in.ClientFamily)
	doc, _ := protocolregistry.ForClientDocumentEncoder(in.ClientFamily)
	stream, _ := protocolregistry.ForClientStreamEncoder(in.ClientFamily)
	providerEncoder, _ := protocolregistry.ForProviderRequestProtocolCarrier(in.ProviderFamily)
	in.ClientCodec = testClientCodec{req: req, doc: doc, stream: stream}
	in.ProviderEncoder = providerEncoder
	if in.ProviderDelivery.Mode == delivery.Streaming {
		streamDecoder, _ := protocolregistry.ForProviderResponseStreamProtocolCarrier(in.ProviderFamily)
		in.StreamDecoder = streamDecoder
	}
	if in.ProviderDelivery.Mode == delivery.Buffered {
		docDecoder, _ := protocolregistry.ForProviderResponseDocumentProtocolCarrierEnvelope(in.ProviderFamily)
		in.DocumentDecoder = docDecoder
	}
	return in
}

type testClientCodec struct {
	req    protocolregistry.ClientRequestDecoder
	doc    protocolregistry.ClientDocumentEncoder
	stream protocolregistry.ClientStreamEncoder
}

func (c testClientCodec) DecodeClientRequest(doc carrier.WireDocument) (canonical.CanonicalRequest, delivery.Delivery, error) {
	return c.req.DecodeClientRequest(doc)
}

func (c testClientCodec) EncodeClientDocument(output canonical.CanonicalOutput) (carrier.WireDocument, error) {
	return c.doc.EncodeClientDocument(output)
}

func (c testClientCodec) EncodeClientStream(events canonical.EventReader) (carrier.WireStream, error) {
	return c.stream.EncodeClientStream(events)
}

func testCanonicalRequest(model string) canonical.CanonicalRequest {
	return canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: model,
		Items: []canonical.CanonicalItem{
			canonical.NewTextItem(canonical.ItemAuthorUser, "hi"),
		},
	})
}
