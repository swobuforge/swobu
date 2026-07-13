package exchange

import (
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/machine"
)

// ---- pure reducers (commands only, zero side effects) ----

func pipelineStartedReduce(in ExchangeInput, _ pipelineStarted) (ExchangeInput, []machine.Event, []machine.Command, error) {
	if strings.TrimSpace(in.Request.Model()) == "" {
		return in, nil, nil, canonical.BadRequest("canonical request is required")
	}
	if err := in.Contract.Validate(); err != nil {
		return in, nil, nil, canonical.BadRequest("execution contract is invalid: " + err.Error())
	}
	if strings.TrimSpace(in.Target.ProviderSpec) == "" {
		return in, nil, nil, canonical.BadEndpoint("provider target is required")
	}
	return in, nil, []machine.Command{machine.Command(resolveCodecs{})}, nil
}

func codecsResolvedReduce(s struct {
	In     ExchangeInput
	Codecs codecResolution
}, _ codecsResolved) (codecResolution, []machine.Event, []machine.Command, error) {
	if !s.Codecs.OK {
		return s.Codecs, nil, nil, canonical.UnsupportedOperation("required codec not resolved")
	}
	return s.Codecs, nil, []machine.Command{machine.Command(encodeProviderRequest{})}, nil
}

func requestEncodedReduce(s encodedRequest, _ requestEncoded) (encodedRequest, []machine.Event, []machine.Command, error) {
	return s, nil, []machine.Command{machine.Command(resolveProviderIngress{})}, nil
}

func ingressReceivedReduce(s struct {
	Ingress providerResponse
	In      ExchangeInput
}, _ ingressReceived) (providerResponse, []machine.Event, []machine.Command, error) {
	if s.Ingress.Ingress == nil {
		return s.Ingress, nil, nil, canonical.InternalError("provider ingress is nil")
	}
	return s.Ingress, nil, []machine.Command{machine.Command(decodeProviderEnvelope{})}, nil
}

func envelopeDecodedReduce(s decodedEnvelope, _ envelopeDecoded) (decodedEnvelope, []machine.Event, []machine.Command, error) {
	return s, nil, []machine.Command{machine.Command(encodeClientOutputCmd{})}, nil
}

func pipelineCompletedReduce(out pipelineOutcome, _ pipelineCompleted) (pipelineOutcome, []machine.Event, []machine.Command, error) {
	return out, nil, nil, nil
}
