package bootstrap

import (
	"context"

	protocolregistry "github.com/swobuforge/swobu/internal/adapters/wire/protocolregistry"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/ports"
)

type exchangeProviderExecutorAdapter struct {
	next ports.ProviderExecutor
}

func newExchangeProviderExecutorAdapter(next ports.ProviderExecutor) exchangeProviderExecutorAdapter {
	return exchangeProviderExecutorAdapter{next: next}
}

func (a exchangeProviderExecutorAdapter) Execute(ctx context.Context, req exchange.ProviderRequest) (exchange.ProviderTransportResponse, error) {
	portsReq := ports.NewProviderRequest(
		req.Request,
		ports.NewExecutionContractForDeliveries(req.Contract.ClientDelivery, req.Contract.ProviderDelivery),
		exchange.NewRoutableTarget(
			req.Target.BackendRef,
			req.Target.ProviderSpec,
			req.Target.BaseURL,
			req.Target.CredentialRef,
			req.Target.ProtocolKind,
			req.Target.AuthKind,
			req.Target.SelectedFrame,
			req.Target.ProviderProtocol,
		),
	)
	portsReq.ProviderWire = req.ProviderWire
	portsResp, err := a.next.Execute(ctx, portsReq)
	if err != nil {
		return exchange.ProviderTransportResponse{}, err
	}
	return exchange.ProviderTransportResponse{
		Header:   portsResp.Header,
		Document: portsResp.Document,
		Stream:   portsResp.Stream,
		Envelope: portsResp.Envelope,
	}, nil
}

type exchangeRuntimeResolver struct{}

func newExchangeRuntimeResolver() exchangeRuntimeResolver { return exchangeRuntimeResolver{} }

func (exchangeRuntimeResolver) ClientCodec(f canonical.IngressFamily) exchange.ClientCodec {
	req, _ := protocolregistry.ForClientRequestDecoder(f)
	doc, _ := protocolregistry.ForClientDocumentEncoder(f)
	stream, _ := protocolregistry.ForClientStreamEncoder(f)
	return clientCodecBundle{request: req, document: doc, stream: stream}
}

func (exchangeRuntimeResolver) ProviderRequestEncoder(kind protocolkind.ProtocolKind) exchange.ProviderRequestEncoder {
	enc, _ := protocolregistry.ForProviderRequestProtocolCarrier(kind)
	return enc
}

func (exchangeRuntimeResolver) ProviderStreamDecoder(kind protocolkind.ProtocolKind, d delivery.Delivery) exchange.ProviderStreamDecoder {
	if d.Mode != delivery.Streaming {
		return nil
	}
	dec, _ := protocolregistry.ForProviderResponseStreamProtocolCarrier(kind)
	return dec
}

func (exchangeRuntimeResolver) ProviderDocumentDecoder(kind protocolkind.ProtocolKind, d delivery.Delivery) exchange.ProviderDocumentDecoder {
	if d.Mode != delivery.Buffered {
		return nil
	}
	dec, _ := protocolregistry.ForProviderResponseDocumentProtocolCarrierEnvelope(kind)
	return dec
}

type clientCodecBundle struct {
	request  protocolregistry.ClientRequestDecoder
	document protocolregistry.ClientDocumentEncoder
	stream   protocolregistry.ClientStreamEncoder
}

func (b clientCodecBundle) DecodeClientRequest(doc carrier.WireDocument) (canonical.CanonicalRequest, delivery.Delivery, error) {
	return b.request.DecodeClientRequest(doc)
}

func (b clientCodecBundle) EncodeClientDocument(output canonical.CanonicalOutput) (carrier.WireDocument, error) {
	return b.document.EncodeClientDocument(output)
}

func (b clientCodecBundle) EncodeClientStream(events canonical.EventReader) (carrier.WireStream, error) {
	return b.stream.EncodeClientStream(events)
}
