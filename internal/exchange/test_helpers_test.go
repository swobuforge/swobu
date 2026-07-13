package exchange

import (
	"context"
	"io"
	"strings"
	"time"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/effect"
)

type recordingEffectSink struct {
	effects []effect.Effect
}

func (s *recordingEffectSink) Commit(_ context.Context, _ string, effects []effect.Effect) error {
	s.effects = append(s.effects, effects...)
	return nil
}

func bufferedProviderIngressResolver(raw []byte) func(context.Context, ProviderRequest) (ProviderIngress, error) {
	_ = raw
	return func(_ context.Context, req ProviderRequest) (ProviderIngress, error) {
		return carrier.NewWireDocument(
			carrier.StageProviderIngressIn,
			req.Target.ProtocolKind,
			"application/json",
			nil,
			[]byte(`{"id":"resp_1","model":"m","output_text":"ok"}`),
			carrier.Meta{},
		), nil
	}
}

func streamingProviderIngressResolver(stream io.ReadCloser) func(context.Context, ProviderRequest) (ProviderIngress, error) {
	_ = stream
	return func(_ context.Context, req ProviderRequest) (ProviderIngress, error) {
		return carrier.WireStream{
			Stage:   carrier.StageProviderIngressIn,
			Family:  req.Target.ProtocolKind,
			Framing: carrier.FramingSSE,
			Frames: carrier.FrameReaderFromReadCloser(io.NopCloser(strings.NewReader(
				"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n" +
					"event: response.completed\ndata: {\"type\":\"response.completed\"}\n\n",
			))),
		}, nil
	}
}

func withRuntime(providerIngress func(context.Context, ProviderRequest) (ProviderIngress, error)) Runner {
	return Runner{Runtime: testExecutionRuntime{
		testRuntimeResolver: testRuntimeResolver{},
		providerIngress:     providerIngress,
	}}
}

func testCanonicalRequest(model string) canonical.CanonicalRequest {
	return canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: model,
		Items: []canonical.CanonicalItem{
			canonical.NewTextItem(canonical.ItemAuthorUser, "hi"),
		},
	})
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
	return testClientCodec{}
}

func (testRuntimeResolver) ProviderRequestDocumentEncoder(kind protocolkind.ProtocolKind) ProviderRequestDocumentEncoder {
	_ = kind
	return testProviderRequestDocumentEncoder{}
}

func (testRuntimeResolver) ProviderEnvelopeDecoder(kind protocolkind.ProtocolKind, d delivery.Delivery) ProviderEnvelopeDecoder {
	_ = kind
	if d.Mode != delivery.Streaming {
		return nil
	}
	return testProviderEnvelopeDecoder{}
}

func (testRuntimeResolver) ProviderDocumentDecoder(kind protocolkind.ProtocolKind, d delivery.Delivery) ProviderDocumentDecoder {
	_ = kind
	if d.Mode != delivery.Buffered {
		return nil
	}
	return testProviderDocumentDecoder{}
}

type testClientCodec struct{}

func (testClientCodec) DecodeClientRequest(doc carrier.WireDocument) (Result[ClientRequestResult], error) {
	_ = doc
	return Result[ClientRequestResult]{}, nil
}

func (testClientCodec) EncodeResponseDocument(output canonical.CanonicalOutput) (Result[carrier.WireDocument], error) {
	text := ""
	if textual, ok := any(output).(interface{ Text() string }); ok {
		text = textual.Text()
	}
	if text == "" {
		for _, item := range output.Items() {
			if item.Kind == canonical.ItemKindText {
				text += item.Text
			}
		}
	}
	if text == "" {
		text = "ok"
	}
	return NewResult(carrier.NewWireDocument("", protocolkind.Responses, "application/json", nil, []byte(`{"output_text":"`+text+`"}`), carrier.Meta{})), nil
}

func (testClientCodec) EncodeResponseStream(events canonical.EventReader, d delivery.Delivery) (Result[carrier.WireStream], error) {
	_ = events
	_ = d
	raw := "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\"}\n\n"
	return NewResult(carrier.WireStream{
		Stage:   carrier.StageClientResponseOut,
		Family:  protocolkind.Responses,
		Framing: carrier.FramingSSE,
		Frames:  carrier.FrameReaderFromReadCloser(io.NopCloser(strings.NewReader(raw))),
	}), nil
}

type testProviderRequestDocumentEncoder struct{}

func (testProviderRequestDocumentEncoder) EncodeProviderRequestDocument(request canonical.CanonicalRequest, d delivery.Delivery, exchangeID string) (Result[carrier.WireDocument], error) {
	_ = d
	_ = exchangeID
	return NewResult(carrier.NewWireDocument(carrier.StageProviderRequestOut, protocolkind.Responses, "application/json", nil, []byte(`{"model":"`+request.Model()+`"}`), carrier.Meta{})), nil
}

type testProviderEnvelopeDecoder struct{}

func (testProviderEnvelopeDecoder) DecodeProviderEnvelope(stream carrier.WireStream, exchangeID string) (Result[canonical.EventReader], error) {
	_ = stream
	return NewResult(stubResponseEventReader(exchangeID)), nil
}

type testProviderDocumentDecoder struct{}

func (testProviderDocumentDecoder) DecodeProviderDocument(ctx context.Context, doc carrier.WireDocument, exchangeID string) (Result[canonical.EventReader], error) {
	_ = ctx
	_ = doc
	return NewResult(stubResponseEventReader(exchangeID)), nil
}

func stubResponseEventReader(exchangeID string) canonical.EventReader {
	now := time.Now().UTC()
	events := []canonical.Event{
		{
			ExchangeID: exchangeID,
			Seq:        1,
			Time:       now,
			Kind:       canonical.EventEnvelopeStart,
			EnvID:      "r1",
			Payload:    canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse},
		},
		{
			ExchangeID: exchangeID,
			Seq:        2,
			Time:       now,
			Kind:       canonical.EventEnvelopeStart,
			EnvID:      "m1",
			ParentID:   "r1",
			Payload:    canonical.EnvelopeStartPayload{Kind: canonical.EnvMessage, Role: canonical.ItemAuthorAssistant},
		},
		{
			ExchangeID: exchangeID,
			Seq:        3,
			Time:       now,
			Kind:       canonical.EventTextDelta,
			EnvID:      "m1",
			Payload:    canonical.TextDeltaPayload{Text: "ok"},
		},
		{
			ExchangeID: exchangeID,
			Seq:        4,
			Time:       now,
			Kind:       canonical.EventEnvelopeEnd,
			EnvID:      "m1",
			ParentID:   "r1",
			Payload:    canonical.EnvelopeEndPayload{Kind: canonical.EnvMessage, Status: canonical.EnvelopeStatusCompleted},
		},
		{
			ExchangeID: exchangeID,
			Seq:        5,
			Time:       now,
			Kind:       canonical.EventEnvelopeEnd,
			EnvID:      "r1",
			Payload:    canonical.EnvelopeEndPayload{Kind: canonical.EnvResponse, Status: canonical.EnvelopeStatusCompleted},
		},
	}
	return canonical.NewSliceEventReader(events)
}
