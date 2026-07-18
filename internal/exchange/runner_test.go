package exchange_test

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
	. "github.com/swobuforge/swobu/internal/exchange"
	stage "github.com/swobuforge/swobu/internal/exchange/stage"
	"github.com/swobuforge/swobu/internal/replay"
	"github.com/swobuforge/swobu/internal/wire"
	chatcompletions "github.com/swobuforge/swobu/internal/wire/chatcompletions"
	messages "github.com/swobuforge/swobu/internal/wire/messages"
	responses "github.com/swobuforge/swobu/internal/wire/responses"
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

type deterministicResponseIDGenerator struct{}

func (deterministicResponseIDGenerator) NewResponseID(_ context.Context, exchangeID string) (replay.ResponseID, error) {
	return replay.ResponseID("swobu_" + exchangeID), nil
}

func TestRunnerRun_BufferedEndToEnd(t *testing.T) {
	runner := withRuntime(bufferedProviderIngressResolver([]byte(`{"id":"resp_1","model":"m","output_text":"ok"}`)))
	out, err := runner.Run(context.Background(), ExchangeInput{
		ExchangeID:       "ex_test",
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.BufferedDelivery(),
		Request:          testCanonicalRequest("m"),
		ReplayScope:      testReplayScope(),
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.BufferedDelivery(),
		Target:           NewRoutableTarget("openai", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "", "responses"),
		Contract:         NewExecutionContract(delivery.BufferedDelivery()),
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if out.Transport.Body == nil {
		t.Fatalf("buffered response must set transport body")
	}
	raw, readErr := io.ReadAll(out.Transport.Body)
	if readErr != nil {
		t.Fatalf("read buffered body: %v", readErr)
	}
	if !strings.Contains(string(raw), "ok") {
		t.Fatalf("ingress body missing expected text: %s", string(raw))
	}
}

func TestRunnerRun_StreamingEndToEnd(t *testing.T) {
	providerSSE := "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"status\":\"in_progress\",\"output\":[]}}\n\n" +
		"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"response_id\":\"resp_1\",\"item_id\":\"msg_1\",\"delta\":\"ok\"}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"status\":\"completed\",\"output\":[{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"ok\"}]}]}}\n\n"
	runner := withRuntime(streamingProviderIngressResolver(io.NopCloser(strings.NewReader(providerSSE))))
	out, err := runner.Run(context.Background(), ExchangeInput{
		ExchangeID:       "ex_test_stream",
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.StreamingDelivery(delivery.FramingSSE),
		Request:          testCanonicalRequest("m"),
		ReplayScope:      testReplayScope(),
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.StreamingDelivery(delivery.FramingSSE),
		Target:           NewRoutableTarget("openai", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "", "responses_stream"),
		Contract:         NewExecutionContract(delivery.StreamingDelivery(delivery.FramingSSE)),
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if out.Transport.Body == nil {
		t.Fatalf("ingress stream body was nil")
	}
	if !out.Progressive {
		t.Fatal("streaming response should remain progressive without buffering wrapper")
	}
	streamRaw, readErr := io.ReadAll(out.Transport.Body)
	if readErr != nil {
		t.Fatalf("read stream: %v", readErr)
	}
	if !strings.Contains(string(streamRaw), "response.completed") {
		t.Fatalf("ingress stream missing terminal marker: %s", string(streamRaw))
	}
}

func TestRunnerRun_StreamingWebSocketPreservesJsonTransport(t *testing.T) {
	runner := withRuntime(bufferedProviderIngressResolver([]byte(`{"id":"resp_1","model":"m","output_text":"ok"}`)))
	out, err := runner.Run(context.Background(), ExchangeInput{
		ExchangeID:       "ex_test_websocket_stream",
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.StreamingDelivery(delivery.FramingWebSocket),
		Request:          testCanonicalRequest("m"),
		ReplayScope:      testReplayScope(),
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.BufferedDelivery(),
		Target:           NewRoutableTarget("openai", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "", "responses"),
		Contract:         NewExecutionContractForDeliveries(delivery.StreamingDelivery(delivery.FramingWebSocket), delivery.BufferedDelivery()),
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := out.Transport.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("content-type = %q, want application/json", got)
	}
	raw, readErr := io.ReadAll(out.Transport.Body)
	if readErr != nil {
		t.Fatalf("read websocket stream: %v", readErr)
	}
	if !strings.Contains(string(raw), `"type":"response.completed"`) {
		t.Fatalf("websocket stream missing completion event: %s", string(raw))
	}
}

func TestRunnerRun_StreamingEndToEnd_DisablesProgressiveWhenWrapperBuffersResponse(t *testing.T) {
	providerSSE := "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"status\":\"in_progress\",\"output\":[]}}\n\n" +
		"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"response_id\":\"resp_1\",\"item_id\":\"msg_1\",\"delta\":\"ok\"}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"status\":\"completed\",\"output\":[{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"ok\"}]}]}}\n\n"
	runner := withRuntime(streamingProviderIngressResolver(io.NopCloser(strings.NewReader(providerSSE))))
	runner.StageMechanics = stage.NewStageMechanics(nil, []stage.EventStreamWrapper{
		bufferingResponseWrapper{},
	})
	out, err := runner.Run(context.Background(), ExchangeInput{
		ExchangeID:       "ex_test_stream_buffered_wrapper",
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.StreamingDelivery(delivery.FramingSSE),
		Request:          testCanonicalRequest("m"),
		ReplayScope:      testReplayScope(),
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.StreamingDelivery(delivery.FramingSSE),
		Target:           NewRoutableTarget("openai", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "", "responses_stream"),
		Contract:         NewExecutionContract(delivery.StreamingDelivery(delivery.FramingSSE)),
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if out.Progressive {
		t.Fatal("buffering wrapper should disable progressive streaming truth")
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

	runner := withRuntime(streamingProviderIngressResolver(providerRead))
	out, err := runner.Run(context.Background(), ExchangeInput{
		ExchangeID:       "ex_test_stream_non_blocking",
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.StreamingDelivery(delivery.FramingSSE),
		Request:          testCanonicalRequest("m"),
		ReplayScope:      testReplayScope(),
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.StreamingDelivery(delivery.FramingSSE),
		Target:           NewRoutableTarget("openai", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "", "responses_stream"),
		Contract:         NewExecutionContract(delivery.StreamingDelivery(delivery.FramingSSE)),
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	readDone := make(chan []byte, 1)
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

func TestRunnerRun_RejectsAmbiguousProviderIngress(t *testing.T) {
	runner := withRuntime(func(context.Context, ProviderRequest) (ProviderIngress, error) {
		return nil, nil
	})
	_, err := runner.Run(context.Background(), ExchangeInput{
		ExchangeID:       "ex_invalid_transport_shape",
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.BufferedDelivery(),
		Request:          testCanonicalRequest("m"),
		ReplayScope:      testReplayScope(),
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.BufferedDelivery(),
		Target:           NewRoutableTarget("openai", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "", "responses"),
		Contract:         NewExecutionContract(delivery.BufferedDelivery()),
	})
	if err == nil {
		t.Fatal("Run() expected error for invalid provider ingress")
	}
	if !strings.Contains(err.Error(), "provider ingress shape is invalid") {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestRunnerRun_RejectsBufferedDeliveryWithTransportStream(t *testing.T) {
	runner := withRuntime(streamingProviderIngressResolver(io.NopCloser(strings.NewReader("event: response.completed\ndata: {}\n\n"))))
	_, err := runner.Run(context.Background(), ExchangeInput{
		ExchangeID:       "ex_invalid_delivery_shape",
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.BufferedDelivery(),
		Request:          testCanonicalRequest("m"),
		ReplayScope:      testReplayScope(),
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.BufferedDelivery(),
		Target:           NewRoutableTarget("openai", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "", "responses"),
		Contract:         NewExecutionContract(delivery.BufferedDelivery()),
	})
	if err == nil {
		t.Fatal("Run() expected error for buffered delivery with transport stream")
	}
	if !strings.Contains(err.Error(), "provider wire stream requires streaming delivery") {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestStreamingClientChatCompletionsFirstFrameBeforeEnvelopeEOF(t *testing.T) {
	providerRead, providerWrite := io.Pipe()
	defer func() { _ = providerRead.Close() }()
	release := make(chan struct{})
	go func() {
		defer func() { _ = providerWrite.Close() }()
		_, _ = io.WriteString(providerWrite, "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"status\":\"in_progress\",\"output\":[]}}\n\n")
		<-release
		_, _ = io.WriteString(providerWrite, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"status\":\"completed\",\"output\":[]}}\n\n")
	}()
	runner := withRuntime(streamingProviderIngressResolver(providerRead))
	out, err := runner.Run(context.Background(), ExchangeInput{
		ExchangeID:       "ex_chat_stream_non_blocking",
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.StreamingDelivery(delivery.FramingSSE),
		Request:          testCanonicalRequest("m"),
		ReplayScope:      testReplayScope(),
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.StreamingDelivery(delivery.FramingSSE),
		Target:           NewRoutableTarget("openai", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "", "responses_stream"),
		Contract:         NewExecutionContract(delivery.StreamingDelivery(delivery.FramingSSE)),
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	readDone := make(chan []byte, 1)
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

func bufferedProviderIngressResolver(raw []byte) func(context.Context, ProviderRequest) (ProviderIngress, error) {
	return func(_ context.Context, req ProviderRequest) (ProviderIngress, error) {
		return carrier.NewCarrierDocument(
			carrier.StageProviderIngressIn,
			req.Target.ProtocolKind,
			"application/json",
			nil,
			raw,
			carrier.Meta{},
		), nil
	}
}

func streamingProviderIngressResolver(stream io.ReadCloser) func(context.Context, ProviderRequest) (ProviderIngress, error) {
	return func(_ context.Context, req ProviderRequest) (ProviderIngress, error) {
		return carrier.CarrierStream{
			Stage:   carrier.StageProviderIngressIn,
			Family:  req.Target.ProtocolKind,
			Framing: carrier.Framing(req.Contract.ProviderDelivery.Framing),
			Frames:  carrier.FrameReaderFromReadCloser(stream),
		}, nil
	}
}

type bufferingResponseWrapper struct{}

func (bufferingResponseWrapper) ID() string { return "test.buffering" }

func (bufferingResponseWrapper) Stage() stage.Stage {
	return stage.StageSemanticEvents
}

func (bufferingResponseWrapper) Capabilities() stage.StageCapabilities {
	return stage.StageCapabilities{BuffersResponse: true}
}

func (bufferingResponseWrapper) Match(stage.Context, canonical.EventReader) bool {
	return true
}

func (bufferingResponseWrapper) Wrap(_ stage.Context, reader canonical.EventReader) (stage.Result[canonical.EventReader], error) {
	return stage.Result[canonical.EventReader]{Value: reader}, nil
}

func withRuntime(providerIngress func(context.Context, ProviderRequest) (ProviderIngress, error)) Runner {
	return Runner{
		Runtime: testExecutionRuntime{
			testRuntimeResolver: testRuntimeResolver{},
			providerIngress:     providerIngress,
		},
		ReplayStore: replay.NewMemoryStore(),
		ResponseIDs: deterministicResponseIDGenerator{},
	}
}

type testExecutionRuntime struct {
	testRuntimeResolver
	providerIngress func(context.Context, ProviderRequest) (ProviderIngress, error)
}

func (r testExecutionRuntime) ResolveProviderIngress(ctx context.Context, req ProviderRequest) (ProviderIngress, error) {
	if r.providerIngress == nil {
		return nil, canonical.InternalError("test provider ingress resolver is required")
	}
	return r.providerIngress(ctx, req)
}

type testRuntimeResolver struct{}

func (testRuntimeResolver) ClientCodec(f canonical.ClientFamily) ClientCodec {
	switch f {
	case canonical.ClientFamilyChatCompletions:
		return testClientCodec{
			req:    chatcompletions.ClientRequestDecoder{},
			doc:    chatcompletions.ResponseDocumentEncoder{},
			stream: chatcompletions.ResponseStreamEncoder{},
		}
	case canonical.ClientFamilyResponses:
		return testClientCodec{
			req:    responses.ClientRequestDecoder{},
			doc:    responses.ResponseDocumentEncoder{},
			stream: responses.ResponseStreamEncoder{},
		}
	case canonical.ClientFamilyMessages:
		return testClientCodec{
			req:    messages.ClientRequestDecoder{},
			doc:    messages.ResponseDocumentEncoder{},
			stream: messages.ResponseStreamEncoder{},
		}
	default:
		return nil
	}
}

func (testRuntimeResolver) ProviderRequestDocumentEncoder(kind protocolkind.ProtocolKind) ProviderRequestDocumentEncoder {
	switch kind {
	case protocolkind.ChatCompletions:
		return chatcompletions.ProviderRequestDocumentEncoder{}
	case protocolkind.Responses:
		return responses.ProviderRequestDocumentEncoder{}
	case protocolkind.Messages:
		return messages.ProviderRequestDocumentEncoder{}
	default:
		return nil
	}
}

func (testRuntimeResolver) ProviderEnvelopeDecoder(kind protocolkind.ProtocolKind, d delivery.Delivery) ProviderEnvelopeDecoder {
	if d.Mode != delivery.Streaming {
		return nil
	}
	switch kind {
	case protocolkind.ChatCompletions:
		return chatcompletions.ProviderEnvelopeDecoder{}
	case protocolkind.Responses:
		return responses.ProviderEnvelopeDecoder{}
	case protocolkind.Messages:
		return messages.ProviderEnvelopeDecoder{}
	default:
		return nil
	}
}

func (testRuntimeResolver) ProviderDocumentDecoder(kind protocolkind.ProtocolKind, d delivery.Delivery) ProviderDocumentDecoder {
	if d.Mode != delivery.Buffered {
		return nil
	}
	switch kind {
	case protocolkind.ChatCompletions:
		return chatcompletions.ProviderDocumentDecoder{}
	case protocolkind.Responses:
		return responses.ProviderDocumentDecoder{}
	case protocolkind.Messages:
		return messages.ProviderDocumentDecoder{}
	default:
		return nil
	}
}

type testClientCodec struct {
	req interface {
		DecodeClientRequest(carrier.CarrierDocument) (Result[wire.ClientRequestResult], error)
	}
	doc interface {
		EncodeResponseDocument(canonical.CanonicalOutput) (Result[carrier.CarrierDocument], error)
	}
	stream interface {
		EncodeResponseStream(canonical.EventReader, delivery.Delivery) (Result[carrier.CarrierStream], error)
	}
}

func (c testClientCodec) DecodeClientRequest(doc carrier.CarrierDocument) (Result[wire.ClientRequestResult], error) {
	return c.req.DecodeClientRequest(doc)
}

func (c testClientCodec) EncodeResponseDocument(output canonical.CanonicalOutput) (Result[carrier.CarrierDocument], error) {
	return c.doc.EncodeResponseDocument(output)
}

func (c testClientCodec) EncodeResponseStream(events canonical.EventReader, d delivery.Delivery) (Result[carrier.CarrierStream], error) {
	return c.stream.EncodeResponseStream(events, d)
}

func testCanonicalRequest(model string) canonical.CanonicalRequest {
	return canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: model,
		Items: []canonical.CanonicalItem{
			canonical.NewTextItem(canonical.ItemAuthorUser, "hi"),
		},
	})
}
