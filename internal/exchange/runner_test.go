package exchange_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	. "github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/provider"
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

type deterministicSwobuResponseIDGenerator struct{}

func (deterministicSwobuResponseIDGenerator) NewSwobuResponseID(_ context.Context, exchangeID string) (canonical.SwobuResponseID, error) {
	return canonical.SwobuResponseID("swobu_" + exchangeID), nil
}

func TestRunnerRun_BufferedEndToEnd(t *testing.T) {
	runner := withRuntime(bufferedProviderTransport([]byte(`{"id":"resp_1","model":"m","output_text":"ok"}`)))
	out, err := RunPreparedProviderForTest(context.Background(), runner, ExchangeInput{
		ExchangeID:       "ex_test",
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.BufferedDelivery(),
		Request:          testCanonicalRequest("m"),
		WorkspaceSlug:    testWorkspaceSlug(),
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.BufferedDelivery(),
		Target:           provider.NewTargetSnapshot("openai", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "responses"),
		Contract:         NewExecutionContract(delivery.BufferedDelivery()),
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if ClientTransportForTest(out).Body == nil {
		t.Fatalf("buffered response must set transport body")
	}
	raw, readErr := io.ReadAll(ClientTransportForTest(out).Body)
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
	runner := withRuntime(streamingProviderTransport(io.NopCloser(strings.NewReader(providerSSE))))
	out, err := RunPreparedProviderForTest(context.Background(), runner, ExchangeInput{
		ExchangeID:       "ex_test_stream",
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.StreamingDelivery(delivery.FramingSSE),
		Request:          testCanonicalRequest("m"),
		WorkspaceSlug:    testWorkspaceSlug(),
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.StreamingDelivery(delivery.FramingSSE),
		Target:           provider.NewTargetSnapshot("openai", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "responses_stream"),
		Contract:         NewExecutionContract(delivery.StreamingDelivery(delivery.FramingSSE)),
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if ClientTransportForTest(out).Body == nil {
		t.Fatalf("ingress stream body was nil")
	}
	if !ClientResponseStreamingForTest(out) {
		t.Fatal("streaming response should remain incremental without buffering wrapper")
	}
	streamRaw, readErr := io.ReadAll(ClientTransportForTest(out).Body)
	if readErr != nil {
		t.Fatalf("read stream: %v", readErr)
	}
	if !strings.Contains(string(streamRaw), "response.completed") {
		t.Fatalf("ingress stream missing terminal marker: %s", string(streamRaw))
	}
}

func TestRunnerRun_StreamingWebSocketPreservesJsonTransport(t *testing.T) {
	runner := withRuntime(bufferedProviderTransport([]byte(`{"id":"resp_1","model":"m","output_text":"ok"}`)))
	out, err := RunPreparedProviderForTest(context.Background(), runner, ExchangeInput{
		ExchangeID:       "ex_test_websocket_stream",
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.StreamingDelivery(delivery.FramingWebSocket),
		Request:          testCanonicalRequest("m"),
		WorkspaceSlug:    testWorkspaceSlug(),
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.BufferedDelivery(),
		Target:           provider.NewTargetSnapshot("openai", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "responses"),
		Contract:         NewExecutionContractForDeliveries(delivery.StreamingDelivery(delivery.FramingWebSocket), delivery.BufferedDelivery()),
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	messageResponse := ClientMessageTransportForTest(out)
	if got := messageResponse.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("content-type = %q, want application/json", got)
	}
	var messages [][]byte
	for {
		message, nextErr := messageResponse.Messages.Next(context.Background())
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			t.Fatalf("read websocket messages: %v", nextErr)
		}
		messages = append(messages, message)
	}
	joined := string(bytes.Join(messages, nil))
	if !strings.Contains(joined, `"type":"response.completed"`) {
		t.Fatalf("websocket messages missing completion event: %s", joined)
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

	runner := withRuntime(streamingProviderTransport(providerRead))
	out, err := RunPreparedProviderForTest(context.Background(), runner, ExchangeInput{
		ExchangeID:       "ex_test_stream_non_blocking",
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.StreamingDelivery(delivery.FramingSSE),
		Request:          testCanonicalRequest("m"),
		WorkspaceSlug:    testWorkspaceSlug(),
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.StreamingDelivery(delivery.FramingSSE),
		Target:           provider.NewTargetSnapshot("openai", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "responses_stream"),
		Contract:         NewExecutionContract(delivery.StreamingDelivery(delivery.FramingSSE)),
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	readDone := make(chan []byte, 1)
	readErr := make(chan error, 1)
	streamBody := ClientTransportForTest(out).Body
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
	runner := withRuntime(func(context.Context, provider.TargetSnapshot, carrier.Document) (provider.Ingress, error) {
		return nil, nil
	})
	_, err := RunPreparedProviderForTest(context.Background(), runner, ExchangeInput{
		ExchangeID:       "ex_invalid_transport_shape",
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.BufferedDelivery(),
		Request:          testCanonicalRequest("m"),
		WorkspaceSlug:    testWorkspaceSlug(),
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.BufferedDelivery(),
		Target:           provider.NewTargetSnapshot("openai", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "responses"),
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
	runner := withRuntime(streamingProviderTransport(io.NopCloser(strings.NewReader("event: response.completed\ndata: {}\n\n"))))
	_, err := RunPreparedProviderForTest(context.Background(), runner, ExchangeInput{
		ExchangeID:       "ex_invalid_delivery_shape",
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.BufferedDelivery(),
		Request:          testCanonicalRequest("m"),
		WorkspaceSlug:    testWorkspaceSlug(),
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.BufferedDelivery(),
		Target:           provider.NewTargetSnapshot("openai", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "responses"),
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
	runner := withRuntime(streamingProviderTransport(providerRead))
	out, err := RunPreparedProviderForTest(context.Background(), runner, ExchangeInput{
		ExchangeID:       "ex_chat_stream_non_blocking",
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.StreamingDelivery(delivery.FramingSSE),
		Request:          testCanonicalRequest("m"),
		WorkspaceSlug:    testWorkspaceSlug(),
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.StreamingDelivery(delivery.FramingSSE),
		Target:           provider.NewTargetSnapshot("openai", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "responses_stream"),
		Contract:         NewExecutionContract(delivery.StreamingDelivery(delivery.FramingSSE)),
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	readDone := make(chan []byte, 1)
	readErr := make(chan error, 1)
	streamBody := ClientTransportForTest(out).Body
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

func bufferedProviderTransport(raw []byte) testProviderTransport {
	return func(_ context.Context, target provider.TargetSnapshot, _ carrier.Document) (provider.Ingress, error) {
		return provider.DocumentIngress{Document: carrier.NewDocument(
			target.ProtocolKind,
			"application/json",
			nil,
			raw,
			carrier.Meta{},
		)}, nil
	}
}

func streamingProviderTransport(stream io.ReadCloser) testProviderTransport {
	return func(_ context.Context, target provider.TargetSnapshot, _ carrier.Document) (provider.Ingress, error) {
		return provider.StreamIngress{Stream: carrier.ByteStream{MediaType: "text/event-stream",
			Body: stream,
		}}, nil
	}
}

func withRuntime(providerTransport testProviderTransport) Runner {
	return Runner{
		Runtime: testExecutionRuntime{
			testRuntimeResolver: testRuntimeResolver{},
			providerTransport:   providerTransport,
		},
		ReplayStore:      replay.NewMemoryStore(),
		SwobuResponseIDs: deterministicSwobuResponseIDGenerator{},
	}
}

type testExecutionRuntime struct {
	testRuntimeResolver
	providerTransport testProviderTransport
}

func (r testExecutionRuntime) ResolveBackend(target provider.TargetSnapshot) (provider.Backend, error) {
	if target.Model == "" {
		target.Model = "m"
	}
	return provider.Backend{Target: target, Codec: testBackendCodec{protocol: target.ProtocolKind}, Transport: provider.BindTransport(target, r.providerTransport)}, nil
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

type testProviderTransport func(context.Context, provider.TargetSnapshot, carrier.Document) (provider.Ingress, error)

func (t testProviderTransport) Send(ctx context.Context, target provider.TargetSnapshot, doc carrier.Document) (provider.Ingress, error) {
	if t == nil {
		return nil, canonical.InternalError("test provider transport is required")
	}
	return t(ctx, target, doc)
}

type testBackendCodec struct{ protocol protocolkind.ProtocolKind }

func (c testBackendCodec) Encode(req provider.Request) (carrier.Document, []compat.Decision, error) {
	input := wire.ProviderEncodeInput{Request: req.Canonical}
	var result wire.ProviderEncodeResult
	var err error
	switch c.protocol {
	case protocolkind.ChatCompletions:
		result, err = (chatcompletions.ProviderRequestDocumentEncoder{}).EncodeProviderRequestWithTokenField(input, req.Delivery, "", chatcompletions.MaxOutputTokensFieldCompletion)
	case protocolkind.Responses:
		result, err = (responses.ProviderRequestDocumentEncoder{}).EncodeProviderRequestDocument(input, req.Delivery, "")
	case protocolkind.Messages:
		result, err = (messages.ProviderRequestDocumentEncoder{}).EncodeProviderRequestDocument(input, req.Delivery, "")
	default:
		return carrier.Document{}, nil, canonical.BadEndpoint("test protocol is unsupported")
	}
	return result.Document, result.Decisions, err
}

func (c testBackendCodec) Decode(ctx context.Context, exchangeID string, ingress provider.Ingress) (provider.DecodedResponse, error) {
	switch in := ingress.(type) {
	case provider.StreamIngress:
		var result wire.ProviderDecodeResult
		var err error
		switch c.protocol {
		case protocolkind.ChatCompletions:
			result, err = (chatcompletions.ProviderEnvelopeDecoder{}).DecodeProviderEnvelope(in.Stream, exchangeID)
		case protocolkind.Responses:
			result, err = (responses.ProviderEnvelopeDecoder{}).DecodeProviderEnvelope(in.Stream, exchangeID)
		case protocolkind.Messages:
			result, err = (messages.ProviderEnvelopeDecoder{}).DecodeProviderEnvelope(in.Stream, exchangeID)
		}
		return provider.DecodedResponse{Stream: result.Stream, Decisions: result.Decisions, TerminalDecisions: result.TerminalDecisions}, err
	case provider.DocumentIngress:
		var result wire.ProviderDecodeResult
		var err error
		switch c.protocol {
		case protocolkind.ChatCompletions:
			result, err = (chatcompletions.ProviderDocumentDecoder{}).DecodeProviderDocument(ctx, in.Document, exchangeID)
		case protocolkind.Responses:
			result, err = (responses.ProviderDocumentDecoder{}).DecodeProviderDocument(ctx, in.Document, exchangeID)
		case protocolkind.Messages:
			result, err = (messages.ProviderDocumentDecoder{}).DecodeProviderDocument(ctx, in.Document, exchangeID)
		}
		return provider.DecodedResponse{Stream: result.Stream, Decisions: result.Decisions, TerminalDecisions: result.TerminalDecisions}, err
	default:
		return provider.DecodedResponse{}, canonical.InternalError("test provider ingress is unsupported")
	}
}

type testClientCodec struct {
	req interface {
		DecodeClientRequest(carrier.Document) (wire.ClientDecodeResult, error)
	}
	doc interface {
		EncodeResponseDocument(canonical.CanonicalOutput) (wire.ClientDocumentResult, error)
	}
	stream interface {
		EncodeResponseStream(context.Context, canonical.ResponseStream, delivery.Delivery) (wire.ClientByteStreamResult, error)
		EncodeResponseMessages(context.Context, canonical.ResponseStream, delivery.Delivery) (wire.ClientMessageResult, error)
	}
}

func (c testClientCodec) DecodeClientRequest(doc carrier.Document) (wire.ClientDecodeResult, error) {
	return c.req.DecodeClientRequest(doc)
}

func (c testClientCodec) EncodeResponseDocument(output canonical.CanonicalOutput) (wire.ClientDocumentResult, error) {
	return c.doc.EncodeResponseDocument(output)
}

func (c testClientCodec) EncodeResponseStream(ctx context.Context, events canonical.ResponseStream, d delivery.Delivery) (wire.ClientByteStreamResult, error) {
	return c.stream.EncodeResponseStream(ctx, events, d)
}

func (c testClientCodec) EncodeResponseMessages(ctx context.Context, events canonical.ResponseStream, d delivery.Delivery) (wire.ClientMessageResult, error) {
	return c.stream.EncodeResponseMessages(ctx, events, d)
}

func testCanonicalRequest(model string) canonical.CanonicalRequest {
	return canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: model,
		Items: []canonical.CanonicalItem{
			canonical.NewTextItem(canonical.ItemAuthorUser, "hi"),
		},
	})
}
