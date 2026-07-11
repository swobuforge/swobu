package exchange

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	chatcompletions "github.com/swobuforge/swobu/internal/adapters/wire/families/chatcompletions"
	completions "github.com/swobuforge/swobu/internal/adapters/wire/families/completions"
	messages "github.com/swobuforge/swobu/internal/adapters/wire/families/messages"
	responses "github.com/swobuforge/swobu/internal/adapters/wire/families/responses"
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
	runner := Runner{ResolveProviderIngress: bufferedProviderIngressResolver([]byte(`{"id":"resp_1","model":"m","output_text":"ok"}`))}
	out, err := runner.Run(context.Background(), withRuntime(ExchangeInput{
		ExchangeID:       "ex_test",
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.BufferedDelivery(),
		Request:          testCanonicalRequest("m"),
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.BufferedDelivery(),
		Target:           NewRoutableTarget("openai", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "", "responses"),
		Contract:         NewExecutionContract(delivery.BufferedDelivery()),
	}))
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
	runner := Runner{ResolveProviderIngress: streamingProviderIngressResolver(io.NopCloser(strings.NewReader(providerSSE)))}
	out, err := runner.Run(context.Background(), withRuntime(ExchangeInput{
		ExchangeID:       "ex_test_stream",
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.StreamingDelivery(delivery.FramingSSE),
		Request:          testCanonicalRequest("m"),
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.StreamingDelivery(delivery.FramingSSE),
		Target:           NewRoutableTarget("openai", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "", "responses_stream"),
		Contract:         NewExecutionContract(delivery.StreamingDelivery(delivery.FramingSSE)),
	}))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if out.Transport.Body == nil {
		t.Fatalf("ingress stream body was nil")
	}
	streamRaw, readErr := io.ReadAll(out.Transport.Body)
	if readErr != nil {
		t.Fatalf("read stream: %v", readErr)
	}
	if !strings.Contains(string(streamRaw), "response.completed") {
		t.Fatalf("ingress stream missing terminal marker: %s", string(streamRaw))
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

	runner := Runner{ResolveProviderIngress: streamingProviderIngressResolver(providerRead)}
	out, err := runner.Run(context.Background(), withRuntime(ExchangeInput{
		ExchangeID:       "ex_test_stream_non_blocking",
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.StreamingDelivery(delivery.FramingSSE),
		Request:          testCanonicalRequest("m"),
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.StreamingDelivery(delivery.FramingSSE),
		Target:           NewRoutableTarget("openai", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "", "responses_stream"),
		Contract:         NewExecutionContract(delivery.StreamingDelivery(delivery.FramingSSE)),
	}))
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
	runner := Runner{ResolveProviderIngress: func(context.Context, ProviderRequest) (ProviderIngress, error) {
		return nil, nil
	}}
	_, err := runner.Run(context.Background(), withRuntime(ExchangeInput{
		ExchangeID:       "ex_invalid_transport_shape",
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.BufferedDelivery(),
		Request:          testCanonicalRequest("m"),
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.BufferedDelivery(),
		Target:           NewRoutableTarget("openai", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "", "responses"),
		Contract:         NewExecutionContract(delivery.BufferedDelivery()),
	}))
	if err == nil {
		t.Fatal("Run() expected error for invalid provider ingress")
	}
	if !strings.Contains(err.Error(), "provider ingress shape is invalid") {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestRunnerRun_RejectsBufferedDeliveryWithTransportStream(t *testing.T) {
	runner := Runner{ResolveProviderIngress: streamingProviderIngressResolver(io.NopCloser(strings.NewReader("event: response.completed\ndata: {}\n\n")))}
	_, err := runner.Run(context.Background(), withRuntime(ExchangeInput{
		ExchangeID:       "ex_invalid_delivery_shape",
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.BufferedDelivery(),
		Request:          testCanonicalRequest("m"),
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.BufferedDelivery(),
		Target:           NewRoutableTarget("openai", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "", "responses"),
		Contract:         NewExecutionContract(delivery.BufferedDelivery()),
	}))
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
	runner := Runner{ResolveProviderIngress: streamingProviderIngressResolver(providerRead)}
	out, err := runner.Run(context.Background(), withRuntime(ExchangeInput{
		ExchangeID:       "ex_chat_stream_non_blocking",
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.StreamingDelivery(delivery.FramingSSE),
		Request:          testCanonicalRequest("m"),
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.StreamingDelivery(delivery.FramingSSE),
		Target:           NewRoutableTarget("openai", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "", "responses_stream"),
		Contract:         NewExecutionContract(delivery.StreamingDelivery(delivery.FramingSSE)),
	}))
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
		return carrier.NewWireDocument(
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
		return carrier.WireStream{
			Stage:   carrier.StageProviderIngressIn,
			Family:  req.Target.ProtocolKind,
			Framing: toCarrierFraming(req.Contract.ProviderDelivery.Framing),
			Frames:  carrier.FrameReaderFromReadCloser(stream),
		}, nil
	}
}

func withRuntime(in ExchangeInput) ExchangeInput {
	in.ClientCodec = testClientCodec{
		req:    clientRequestDecoderForFamily(in.ClientFamily),
		doc:    responseDocumentEncoderForFamily(in.ClientFamily),
		stream: responseStreamEncoderForFamily(in.ClientFamily),
	}
	in.ProviderRequestDocumentEncoder = providerRequestEncoderForProtocol(in.ProviderProtocol)
	if in.ProviderDelivery.Mode == delivery.Streaming {
		in.ProviderEnvelopeDecoder = providerResponseEnvelopeDecoderForProtocol(in.ProviderProtocol)
	}
	if in.ProviderDelivery.Mode == delivery.Buffered {
		in.ProviderDocumentDecoder = providerResponseDocumentDecoderForProtocol(in.ProviderProtocol)
	}
	return in
}

type testClientCodec struct {
	req interface {
		DecodeClientRequest(carrier.WireDocument) (canonical.CanonicalRequest, delivery.Delivery, error)
	}
	doc interface {
		EncodeResponseDocument(canonical.CanonicalOutput) (carrier.WireDocument, error)
	}
	stream interface {
		EncodeResponseStream(canonical.EventReader, delivery.Delivery) (carrier.WireStream, error)
	}
}

func (c testClientCodec) DecodeClientRequest(doc carrier.WireDocument) (canonical.CanonicalRequest, delivery.Delivery, error) {
	return c.req.DecodeClientRequest(doc)
}

func (c testClientCodec) EncodeResponseDocument(output canonical.CanonicalOutput) (carrier.WireDocument, error) {
	return c.doc.EncodeResponseDocument(output)
}

func (c testClientCodec) EncodeResponseStream(events canonical.EventReader, d delivery.Delivery) (carrier.WireStream, error) {
	return c.stream.EncodeResponseStream(events, d)
}

func responseDocumentEncoderForFamily(family canonical.ClientFamily) interface {
	EncodeResponseDocument(canonical.CanonicalOutput) (carrier.WireDocument, error)
} {
	switch family {
	case canonical.ClientFamilyChatCompletions:
		return chatcompletions.ResponseDocumentEncoder{}
	case canonical.ClientFamilyResponses:
		return responses.ResponseDocumentEncoder{}
	case canonical.ClientFamilyCompletions:
		return completions.ResponseDocumentEncoder{}
	case canonical.ClientFamilyMessages:
		return messages.ResponseDocumentEncoder{}
	default:
		panic("test response document encoder missing for family " + string(family))
	}
}

func responseStreamEncoderForFamily(family canonical.ClientFamily) interface {
	EncodeResponseStream(canonical.EventReader, delivery.Delivery) (carrier.WireStream, error)
} {
	switch family {
	case canonical.ClientFamilyChatCompletions:
		return chatcompletions.ResponseStreamEncoder{}
	case canonical.ClientFamilyResponses:
		return responses.ResponseStreamEncoder{}
	case canonical.ClientFamilyCompletions:
		return completions.ResponseStreamEncoder{}
	case canonical.ClientFamilyMessages:
		return messages.ResponseStreamEncoder{}
	default:
		panic("test response stream encoder missing for family " + string(family))
	}
}

func providerRequestEncoderForProtocol(kind protocolkind.ProtocolKind) interface {
	EncodeProviderRequestDocument(canonical.CanonicalRequest, delivery.Delivery) (carrier.WireDocument, error)
} {
	switch kind {
	case protocolkind.ChatCompletions:
		return chatcompletions.ProviderRequestDocumentEncoder{}
	case protocolkind.Responses:
		return responses.ProviderRequestDocumentEncoder{}
	case protocolkind.Completions:
		return completions.ProviderRequestDocumentEncoder{}
	case protocolkind.Messages:
		return messages.ProviderRequestDocumentEncoder{}
	default:
		panic("test provider request encoder missing for protocol " + string(kind))
	}
}

func clientRequestDecoderForFamily(family canonical.ClientFamily) interface {
	DecodeClientRequest(carrier.WireDocument) (canonical.CanonicalRequest, delivery.Delivery, error)
} {
	switch family {
	case canonical.ClientFamilyChatCompletions:
		return chatcompletions.ClientRequestDecoder{}
	case canonical.ClientFamilyResponses:
		return responses.ClientRequestDecoder{}
	case canonical.ClientFamilyCompletions:
		return completions.ClientRequestDecoder{}
	case canonical.ClientFamilyMessages:
		return messages.ClientRequestDecoder{}
	default:
		panic("test client request decoder missing for family " + string(family))
	}
}

func providerResponseDocumentDecoderForProtocol(kind protocolkind.ProtocolKind) interface {
	DecodeProviderDocument(carrier.WireDocument, string) (canonical.EventReader, error)
} {
	switch kind {
	case protocolkind.ChatCompletions:
		return chatcompletions.ProviderDocumentDecoder{}
	case protocolkind.Responses:
		return responses.ProviderDocumentDecoder{}
	case protocolkind.Completions:
		return completions.ProviderDocumentDecoder{}
	case protocolkind.Messages:
		return messages.ProviderDocumentDecoder{}
	default:
		panic("test provider response document decoder missing for protocol " + string(kind))
	}
}

func providerResponseEnvelopeDecoderForProtocol(kind protocolkind.ProtocolKind) interface {
	DecodeProviderEnvelope(carrier.WireStream, string) canonical.EventReader
} {
	switch kind {
	case protocolkind.ChatCompletions:
		return chatcompletions.ProviderEnvelopeDecoder{}
	case protocolkind.Responses:
		return responses.ProviderEnvelopeDecoder{}
	case protocolkind.Completions:
		return completions.ProviderEnvelopeDecoder{}
	case protocolkind.Messages:
		return messages.ProviderEnvelopeDecoder{}
	default:
		panic("test provider response envelope decoder missing for protocol " + string(kind))
	}
}

func testCanonicalRequest(model string) canonical.CanonicalRequest {
	return canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: model,
		Items: []canonical.CanonicalItem{
			canonical.NewTextItem(canonical.ItemAuthorUser, "hi"),
		},
	})
}
