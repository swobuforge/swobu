package exchange

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/continuity"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/historyfingerprint"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/domain/thread"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/routing"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
	"github.com/swobuforge/swobu/internal/wire"
)

// Test-only values keep provider-round tests concise without restoring a
// production provider-only execution surface or shadow call authority.
type Runner = runtimeBundle

const testHistoryScheme historyfingerprint.Scheme = "responses/v1"

func testThreadID(material string) thread.ID {
	id, err := thread.Derive("exchange-test/v1", material)
	if err != nil {
		panic(err)
	}
	return id
}

func testHistoryRequest(material []byte) historyfingerprint.Request {
	fingerprint, err := historyfingerprint.FingerprintRequest(testHistoryScheme, material)
	if err != nil {
		panic(err)
	}
	return fingerprint
}

func testHistoryResponse(material []byte) *historyfingerprint.Response {
	fingerprint, err := historyfingerprint.FingerprintResponse(testHistoryScheme, material)
	if err != nil {
		panic(err)
	}
	return &fingerprint
}

type ExchangeInput struct {
	ExchangeID       string
	ClientFamily     canonical.ClientFamily
	ClientDelivery   delivery.Delivery
	Request          canonical.CanonicalRequest
	Prepared         continuity.ResolvedRequest
	WorkspaceSlug    string
	Target           provider.TargetSnapshot
	Contract         ExecutionContract
	ProviderProtocol protocolkind.ProtocolKind
	ProviderDelivery delivery.Delivery
}

func testDecodedRequest(request canonical.CanonicalRequest) wire.ClientRequestResult {
	return wire.ClientRequestResult{
		Request:            request,
		Delivery:           delivery.BufferedDelivery(),
		RequestFingerprint: testHistoryRequest([]byte("test-request")),
	}
}

func ClientTransportForTest(response ClientResponse) carrier.Response {
	switch response := response.(type) {
	case BufferedResponse:
		return response.Response
	case StreamingResponse:
		return response.Response
	}
	return carrier.Response{}
}

func ClientResponseStreamingForTest(response ClientResponse) bool {
	switch response.(type) {
	case StreamingResponse, MessageStreamingResponse:
		return true
	default:
		return false
	}
}

func ClientMessageTransportForTest(response ClientResponse) carrier.MessageTransportResponse {
	if response, ok := response.(MessageStreamingResponse); ok {
		return response.Response
	}
	return carrier.MessageTransportResponse{}
}

type deterministicResponseIDGenerator struct{}

func (deterministicResponseIDGenerator) NewSwobuResponseID(_ context.Context, exchangeID string) (canonical.SwobuResponseID, error) {
	return canonical.SwobuResponseID("swobu_" + exchangeID), nil
}

func runPreparedProviderForTest(ctx context.Context, runner Runner, in ExchangeInput) (ClientResponse, error) {
	if err := validateCheckpointInput(runner, in.WorkspaceSlug); err != nil {
		return nil, err
	}
	if in.Prepared.Request().Model() == "" {
		prepared, err := continuity.Begin(in.Request)
		if err != nil {
			return nil, err
		}
		in.Prepared = prepared
	}
	responseID, err := allocateResponseID(ctx, in.ExchangeID, runner.ResponseIDs)
	if err != nil {
		return nil, err
	}
	backend, err := runner.Runtime.ResolveBackend(in.Target)
	if err != nil {
		return nil, err
	}
	clientCodec := runner.Runtime.ClientCodec(in.ClientFamily)
	request := provider.Request{Canonical: in.Prepared.Request(), Delivery: in.ProviderDelivery}
	threadID, err := thread.Derive("swobu/genesis-response/v1", in.WorkspaceSlug, responseID.String())
	if err != nil {
		return nil, err
	}
	request.Attempt = provider.AttemptContext{ThreadID: threadID}
	if previous, ok := in.Prepared.PreviousHistory(backend.Target.TargetID, backend.Target.TargetVersion); ok {
		request.PreviousHistory = &previous
	}
	call := providerCall{
		backend: backend, request: request, clientCodec: clientCodec,
		clientDelivery: in.ClientDelivery, exchangeID: in.ExchangeID,
		workspaceSlug: in.WorkspaceSlug, fullRequest: in.Prepared.Request(),
		historyScheme: testHistoryScheme,
		advance:       &historyAdvance{Request: testHistoryRequest([]byte("test-request"))},
		threadID:      threadID,
	}
	document, _, err := backend.Codec.Encode(request)
	call.document = document
	if err != nil {
		return nil, err
	}
	ingress, err := backend.Transport.Send(ctx, document)
	if err != nil {
		return nil, err
	}
	response, _, _, err := completeProviderCall(ctx, call, ingress, responseID, runner)
	return response, err
}

func mustResumeSession(
	t *testing.T,
	previousRequest canonical.CanonicalRequest,
	previousResponseItems []canonical.CanonicalItem,
	current canonical.CanonicalRequest,
	target routing.Target,
) continuity.ResolvedRequest {
	t.Helper()
	path, err := resolveProviderPath(target)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := path.target
	response, err := canonical.NewCanonicalResponse(
		canonical.ResponseRef{
			SwobuID: "swobu_resp_123",
			Responses: &canonical.ResponsesContinuation{
				ProviderResponseID: "provider_resp_789",
				TargetID:           snapshot.TargetID, TargetVersion: snapshot.TargetVersion,
			},
		},
		snapshot.Model,
		previousResponseItems,
		canonical.Completed("completed"),
		canonical.NewUnknownTokenUsage(),
	)
	if err != nil {
		t.Fatal(err)
	}
	current = canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: current.ModelField(), Items: current.Items(),
		PreviousResponse: &canonical.ResponseRef{SwobuID: "swobu_resp_123"},
		ToolPolicy:       current.ToolPolicyField(), ToolCallBatch: current.ToolCallBatchField(),
		Controls: current.Controls(), Reasoning: current.Reasoning(), OutputFormat: current.OutputFormatField(),
		Store: current.StoreField(),
	})
	prepared, err := continuity.Resume(current, continuity.Checkpoint{Request: previousRequest, Response: response})
	if err != nil {
		t.Fatal(err)
	}
	return prepared
}

func mustNativeSession(t *testing.T, current canonical.CanonicalRequest, target routing.Target) continuity.ResolvedRequest {
	t.Helper()
	previous := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("a"),
		Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "complete history")},
	})
	return mustResumeSession(t, previous, nil, current, target)
}

// RunPreparedProviderForTest exposes the package test bridge to external tests.
// Production code has no provider-only execution entrypoint.
func RunPreparedProviderForTest(ctx context.Context, runner Runner, in ExchangeInput) (ClientResponse, error) {
	return runPreparedProviderForTest(ctx, runner, in)
}

func bufferedProviderTransport(raw []byte) testProviderTransport {
	_ = raw
	return func(_ context.Context, target provider.TargetSnapshot, request carrier.Document) (provider.Ingress, error) {
		response := carrier.NewDocument(
			target.ProtocolKind,
			"application/json",
			nil,
			[]byte(`{"id":"resp_1","model":"m","output_text":"ok"}`),
			carrier.Meta{},
		)
		var wireRequest struct {
			Stream bool `json:"stream"`
		}
		if json.Unmarshal(request.RawBytes(), &wireRequest) == nil && wireRequest.Stream {
			return streamingProviderIngressForDocument(target, response), nil
		}
		return provider.DocumentIngress{Document: response}, nil
	}
}

// streamingProviderIngressForDocument supplies a valid protocol-family SSE
// carrier for Exchange tests whose production path requests transient upstream
// streaming. The helper converts only test documents; provider adapters retain
// ownership of real response framing.
func streamingProviderIngressForDocument(target provider.TargetSnapshot, document carrier.Document) provider.Ingress {
	switch target.ProtocolKind {
	case protocolkind.ChatCompletions:
		return provider.StreamIngress{Stream: carrier.ByteStream{
			MediaType: "text/event-stream",
			Body: io.NopCloser(strings.NewReader(
				"data: {\"id\":\"chat_1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"ok\"}}]}\n\n" +
					"data: {\"id\":\"chat_1\",\"model\":\"m\",\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
					"data: [DONE]\n\n",
			)),
		}}
	case protocolkind.Messages:
		return provider.StreamIngress{Stream: carrier.ByteStream{
			MediaType: "text/event-stream",
			Body: io.NopCloser(strings.NewReader(
				"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"m\",\"content\":[]}}\n\n" +
					"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
					"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n" +
					"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
					"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n" +
					"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
			)),
		}}
	case protocolkind.Responses:
		return responsesDocumentAsStream(document)
	default:
		return provider.DocumentIngress{Document: document}
	}
}

func responsesDocumentAsStream(document carrier.Document) provider.Ingress {
	var response struct {
		ID         string            `json:"id"`
		Model      string            `json:"model"`
		Status     string            `json:"status"`
		OutputText string            `json:"output_text"`
		Output     []json.RawMessage `json:"output"`
	}
	_ = json.Unmarshal(document.RawBytes(), &response)
	if response.ID == "" {
		response.ID = "resp_1"
	}
	if response.Model == "" {
		response.Model = "m"
	}
	if response.Status == "" {
		response.Status = "completed"
	}
	var frames strings.Builder
	fmt.Fprintf(&frames, "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":%q,\"model\":%q,\"status\":\"in_progress\",\"output\":[]}}\n\n", response.ID, response.Model)
	if response.OutputText != "" {
		fmt.Fprintf(&frames, "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"response_id\":%q,\"delta\":%q}\n\n", response.ID, response.OutputText)
	}
	for index, item := range response.Output {
		fmt.Fprintf(&frames, "event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":%d,\"item\":%s}\n\n", index, item)
	}
	completed := map[string]any{
		"type": "response.completed",
		"response": map[string]any{
			"id": response.ID, "model": response.Model, "status": response.Status,
			"output": response.Output, "output_text": response.OutputText,
		},
	}
	raw, _ := json.Marshal(completed)
	fmt.Fprintf(&frames, "event: response.completed\ndata: %s\n\n", raw)
	return provider.StreamIngress{Stream: carrier.ByteStream{
		MediaType: "text/event-stream",
		Body:      io.NopCloser(strings.NewReader(frames.String())),
	}}
}

func streamingProviderTransport(stream io.ReadCloser) testProviderTransport {
	return func(_ context.Context, target provider.TargetSnapshot, _ carrier.Document) (provider.Ingress, error) {
		return provider.StreamIngress{Stream: carrier.ByteStream{MediaType: "text/event-stream", Body: stream}}, nil
	}
}

func newTransportRequestWithTurn(method, url string, turn string, body map[string]any) carrier.TransportRequest {
	if body == nil {
		body = make(map[string]any)
	}
	if turn != "" {
		body["previous_response_id"] = turn
	}
	raw, _ := json.Marshal(body)
	return NewTransportRequest(method, url, nil, raw)
}

func withRuntime(providerTransport testProviderTransport) Runner {
	// This helper installs testClientCodec for lifecycle-only tests. Protocol
	// wire assertions must use runtimeWithProviderIngress with the real
	// codecresolver.RuntimeCodecResolver; ClientFamily alone does not replace
	// this fake codec.
	return Runner{
		Runtime: testExecutionRuntime{
			testRuntimeResolver: testRuntimeResolver{},
			providerTransport:   providerTransport,
		},
		CheckpointStore: continuity.NewMemoryStore(),
		ResponseIDs:     deterministicResponseIDGenerator{},
		Policy:          DefaultWorkspacePolicy(),
	}
}

type runtimeWithProviderIngress struct {
	RuntimeResolver
	providerTransport testProviderTransport
}

func (r runtimeWithProviderIngress) ResolveBackend(target provider.TargetSnapshot) (provider.Backend, error) {
	return newTestBackend(target, r.providerTransport)
}

func (r Runner) WithCheckpointStore(store continuity.Store) Runner {
	r.CheckpointStore = store
	return r
}

func (r Runner) WithResponseIDs(gen ResponseIDGenerator) Runner {
	r.ResponseIDs = gen
	return r
}

func testCanonicalRequest(model string) canonical.CanonicalRequest {
	return canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify(model),
		Items: []canonical.CanonicalItem{
			testMessage(canonical.MessageRoleUser, "hi"),
		},
	})
}

func testMessage(author canonical.MessageRole, text string) canonical.CanonicalItem {
	item, err := canonical.NewMessageItem(author, []canonical.MessagePart{canonical.NewTextMessagePart(text)})
	if err != nil {
		panic(err)
	}
	return item
}

type testExecutionRuntime struct {
	testRuntimeResolver
	providerTransport testProviderTransport
}

func (r testExecutionRuntime) ResolveBackend(target provider.TargetSnapshot) (provider.Backend, error) {
	return newTestBackend(target, r.providerTransport)
}

type testRuntimeResolver struct{}

func (testRuntimeResolver) ClientCodec(f canonical.ClientFamily) ClientCodec {
	return testClientCodec{}
}

type testProviderTransport func(context.Context, provider.TargetSnapshot, carrier.Document) (provider.Ingress, error)

func (t testProviderTransport) Send(ctx context.Context, target provider.TargetSnapshot, doc carrier.Document) (provider.Ingress, error) {
	if t == nil {
		return nil, canonical.InternalError("test provider transport is required")
	}
	return t(ctx, target, doc)
}

type testBackendCodec struct{}

func (testBackendCodec) Encode(req provider.Request) (carrier.Document, []compat.Change, error) {
	result, err := (testProviderRequestDocumentEncoder{}).EncodeProviderRequestDocument(wire.ProviderEncodeInput{Request: req.Canonical, ToolNames: req.ToolNames}, req.Delivery, "")
	return result.Document, result.Changes, err
}

func (testBackendCodec) Decode(ctx context.Context, request provider.Request, ingress provider.Ingress) (provider.DecodedResponse, error) {
	exchangeID := request.Attempt.ExchangeID
	switch in := ingress.(type) {
	case provider.StreamIngress:
		result, err := (testProviderEnvelopeDecoder{}).DecodeProviderEnvelope(in.Stream, exchangeID)
		return provider.DecodedResponse{Stream: result.Stream, Changes: result.Changes, ProgressiveChanges: result.ProgressiveChanges}, err
	case provider.DocumentIngress:
		result, err := (testProviderDocumentDecoder{}).DecodeProviderDocument(ctx, in.Document, exchangeID)
		return provider.DecodedResponse{Stream: result.Stream, Changes: result.Changes, ProgressiveChanges: result.ProgressiveChanges}, err
	default:
		return provider.DecodedResponse{}, canonical.InternalError("test provider ingress is unsupported")
	}
}

func newTestBackend(target provider.TargetSnapshot, transport testProviderTransport) (provider.Backend, error) {
	if target.Model == "" {
		target.Model = "m"
	}
	return provider.Backend{
		Target: target,
		Codec:  testBackendCodec{},
		Transport: provider.BindTransport(target, func(ctx context.Context, selected provider.TargetSnapshot, document carrier.Document) (provider.Ingress, error) {
			ingress, err := transport.Send(ctx, selected, document)
			if err != nil {
				return ingress, err
			}
			return normalizeTestProviderIngress(selected, document, ingress), nil
		}),
	}, nil
}

func normalizeTestProviderIngress(target provider.TargetSnapshot, request carrier.Document, ingress provider.Ingress) provider.Ingress {
	if _, streamed := ingress.(provider.DocumentIngress); !streamed {
		return ingress
	}
	var wireRequest struct {
		Stream bool `json:"stream"`
	}
	if json.Unmarshal(request.RawBytes(), &wireRequest) != nil || !wireRequest.Stream {
		return ingress
	}
	return streamingProviderIngressForDocument(target, ingress.(provider.DocumentIngress).Document)
}

func bindTestProviderTransport(target provider.TargetSnapshot, transport testProviderTransport) provider.Transport {
	return provider.BindTransport(target, func(ctx context.Context, selected provider.TargetSnapshot, document carrier.Document) (provider.Ingress, error) {
		ingress, err := transport.Send(ctx, selected, document)
		if err != nil {
			return ingress, err
		}
		return normalizeTestProviderIngress(selected, document, ingress), nil
	})
}

type testClientCodec struct{}

func (testClientCodec) DecodeClientRequest(doc carrier.Document) (wire.ClientDecodeResult, error) {
	model := "m"
	var previousResponse *canonical.ResponseRef
	var items []canonical.CanonicalItem
	if len(doc.Raw) > 0 {
		var parsed map[string]any
		if err := json.Unmarshal(doc.Raw, &parsed); err == nil {
			if v, ok := parsed["model"].(string); ok && v != "" {
				model = v
			}
			if v, ok := parsed["previous_response_id"].(string); ok && v != "" {
				previousResponse = &canonical.ResponseRef{SwobuID: canonical.NewSwobuResponseID(v)}
			}
			if msgs, ok := parsed["messages"].([]any); ok {
				for _, m := range msgs {
					if msg, ok := m.(map[string]any); ok {
						content, _ := msg["content"].(string)
						role, _ := msg["role"].(string)
						var author canonical.MessageRole
						switch role {
						case "assistant":
							author = canonical.MessageRoleAssistant
						default:
							author = canonical.MessageRoleUser
						}
						items = append(items, testMessage(author, content))
					}
				}
			}
		}
	}
	if len(items) == 0 {
		items = []canonical.CanonicalItem{testMessage(canonical.MessageRoleUser, "hi")}
	}
	return wire.ClientDecodeResult{
		Request: wire.ClientRequestResult{
			Request: canonical.NewCanonicalRequest(canonical.RequestParams{
				Model:            canonical.Specify(model),
				Items:            items,
				PreviousResponse: previousResponse,
			}),
			Delivery:           delivery.BufferedDelivery(),
			RequestFingerprint: testHistoryRequest(doc.RawBytes()),
		},
	}, nil
}

func (testClientCodec) EncodeResponseDocument(_ canonical.CanonicalRequest, output canonical.CanonicalResponse) (wire.ClientDocumentResult, error) {
	text := ""
	if textual, ok := any(output).(interface{ Text() string }); ok {
		text = textual.Text()
	}
	if text == "" {
		for _, item := range output.Items() {
			if message, ok := item.Message(); ok {
				for _, part := range message.Content() {
					if value, ok := part.Text(); ok {
						text += value.Text()
					}
				}
			}
		}
	}
	if text == "" {
		text = "ok"
	}
	resultID := output.Response().SwobuID.String()
	if resultID != "" {
		return wire.ClientDocumentResult{Document: carrier.NewDocument(protocolkind.Responses, "application/json", nil, []byte(`{"id":"`+resultID+`","output_text":"`+text+`"}`), carrier.Meta{}), ResponseFingerprint: testHistoryResponse([]byte(text))}, nil
	}
	return wire.ClientDocumentResult{Document: carrier.NewDocument(protocolkind.Responses, "application/json", nil, []byte(`{"output_text":"`+text+`"}`), carrier.Meta{}), ResponseFingerprint: testHistoryResponse([]byte(text))}, nil
}

func (testClientCodec) EncodeResponseStream(ctx context.Context, _ canonical.CanonicalRequest, events canonical.ResponseStream, d delivery.Delivery) (wire.ClientByteStreamResult, error) {
	_ = d
	completion, complete, fail := wire.NewResponseCompletion()
	body := wire.NewEncodedResponseBody(ctx, events, func(event canonical.Event) ([][]byte, error) {
		if status, terminal := responseTerminalStatus(event); terminal {
			if status == canonical.EnvelopeStatusCompleted {
				complete(testHistoryResponse([]byte("ok")), nil)
				return [][]byte{[]byte("event: response.completed\ndata: {\"type\":\"response.completed\"}\n\n")}, nil
			} else {
				fail(errors.New("terminal response failed"))
			}
		}
		return [][]byte{[]byte("event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n")}, nil
	}, completion, fail)
	return wire.ClientByteStreamResult{Stream: carrier.ByteStream{MediaType: "text/event-stream", Body: body}, Completion: completion}, nil
}

func (testClientCodec) EncodeResponseMessages(_ context.Context, _ canonical.CanonicalRequest, events canonical.ResponseStream, _ delivery.Delivery) (wire.ClientMessageResult, error) {
	completion, complete, fail := wire.NewResponseCompletion()
	messages := wire.NewEncodedResponseMessages(events, func(event canonical.Event) ([][]byte, error) {
		if status, terminal := responseTerminalStatus(event); terminal && status == canonical.EnvelopeStatusCompleted {
			complete(testHistoryResponse([]byte("ok")), nil)
		}
		return [][]byte{[]byte(`{"type":"test.event"}`)}, nil
	}, completion, fail)
	return wire.ClientMessageResult{Response: carrier.MessageResponse{MediaType: "application/json", Messages: messages}, Completion: completion}, nil
}

type testProviderRequestDocumentEncoder struct{}

func (testProviderRequestDocumentEncoder) EncodeProviderRequestDocument(input wire.ProviderEncodeInput, d delivery.Delivery, exchangeID string) (wire.ProviderEncodeResult, error) {
	_ = exchangeID
	raw := `{"model":"` + input.Request.Model() + `"`
	if d.IsStreaming() {
		raw += `,"stream":true`
	}
	raw += `}`
	return wire.ProviderEncodeResult{Document: carrier.NewDocument(protocolkind.Responses, "application/json", nil, []byte(raw), carrier.Meta{})}, nil
}

type testProviderEnvelopeDecoder struct{}

func (testProviderEnvelopeDecoder) DecodeProviderEnvelope(stream carrier.ByteStream, exchangeID string) (wire.ProviderDecodeResult, error) {
	_ = stream
	return wire.ProviderDecodeResult{Stream: stubResponseEventReader(exchangeID)}, nil
}

type testProviderDocumentDecoder struct{}

func (testProviderDocumentDecoder) DecodeProviderDocument(ctx context.Context, doc carrier.Document, exchangeID string) (wire.ProviderDecodeResult, error) {
	_ = ctx
	_ = doc
	return wire.ProviderDecodeResult{Stream: stubResponseEventReader(exchangeID)}, nil
}

func stubResponseEventReader(exchangeID string) canonical.ResponseStream {
	now := time.Now().UTC()
	item := testMessage(canonical.MessageRoleAssistant, "ok")
	events := []canonical.Event{
		{
			ExchangeID: exchangeID,
			Seq:        1,
			Time:       now,
			Kind:       canonical.EventEnvelopeStart,
			EnvID:      "r1",
			Payload:    canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse, Model: "m"},
		},
		{
			ExchangeID: exchangeID,
			Seq:        2,
			Time:       now,
			Kind:       canonical.EventResponseIdentity,
			EnvID:      "r1",
			Payload:    canonical.ResponseIdentityPayload{Response: canonical.ResponseRef{SwobuID: canonical.NewSwobuResponseID(exchangeID + "_result")}},
		},
		{
			ExchangeID: exchangeID,
			Seq:        3,
			Time:       now,
			Kind:       canonical.EventItemStart,
			Payload:    canonical.ItemEvent{Position: canonical.ItemPosition{Item: 0}, Payload: canonicaltest.MustMessageStart(canonical.MessageRoleAssistant)},
		},
		{
			ExchangeID: exchangeID,
			Seq:        4,
			Time:       now,
			Kind:       canonical.EventContentStart,
			Payload:    canonical.ItemEvent{Position: canonical.ItemPosition{Item: 0, Part: 0}, Payload: canonical.NewMessageContentStart(canonical.PartKindText)},
		},
		{
			ExchangeID: exchangeID,
			Seq:        5,
			Time:       now,
			Kind:       canonical.EventTextDelta,
			Payload:    canonical.ItemEvent{Position: canonical.ItemPosition{Item: 0, Part: 0}, Payload: canonical.TextDeltaPayload{Text: "ok"}},
		},
		{
			ExchangeID: exchangeID,
			Seq:        6,
			Time:       now,
			Kind:       canonical.EventItemCompleted,
			Payload:    canonical.ItemEvent{Position: canonical.ItemPosition{Item: 0}, Payload: canonical.ItemCompletedPayload{Item: item}},
		},
		{
			ExchangeID: exchangeID,
			Seq:        7,
			Time:       now,
			Kind:       canonical.EventFinish,
			EnvID:      "r1",
			Payload:    canonical.FinishPayload{Completion: canonical.Completed("stop")},
		},
		{
			ExchangeID: exchangeID,
			Seq:        8,
			Time:       now,
			Kind:       canonical.EventEnvelopeEnd,
			EnvID:      "r1",
			Payload:    canonical.EnvelopeEndPayload{Kind: canonical.EnvResponse, Status: canonical.EnvelopeStatusCompleted},
		},
	}
	return canonical.NewSliceEventReader(events)
}
