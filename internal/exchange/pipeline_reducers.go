package exchange

import (
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/machine"
)

// ---- pure reducers (commands only, zero side effects) ----

func pipelineStartedReduce(in ExchangeInput, _ PipelineStartedEvent) (ExchangeInput, []machine.Event, []machine.Command, error) {
	if strings.TrimSpace(in.Request.Model()) == "" { // swobu:io-string source=boundary
		return in, nil, nil, canonical.BadRequest("canonical request is required")
	}
	if err := in.Contract.Validate(); err != nil {
		return in, nil, nil, canonical.BadRequest("execution contract is invalid: " + err.Error())
	}
	if strings.TrimSpace(in.Target.ProviderSpec) == "" { // swobu:io-string source=boundary
		return in, nil, nil, canonical.BadEndpoint("provider target is required")
	}
	return in, nil, []machine.Command{machine.Command(ResolveCodecsAction{})}, nil
}

func codecsResolvedReduce(s struct {
	In     ExchangeInput
	Codecs codecResolutionState
}, _ CodecsResolvedEvent) (codecResolutionState, []machine.Event, []machine.Command, error) {
	if !s.Codecs.OK {
		return s.Codecs, nil, nil, canonical.UnsupportedOperation("required codec not resolved")
	}
	return s.Codecs, nil, []machine.Command{machine.Command(EncodeProviderRequestAction{})}, nil
}

func requestEncodedReduce(s EncodedRequestState, _ RequestEncodedEvent) (EncodedRequestState, []machine.Event, []machine.Command, error) {
	return s, nil, []machine.Command{machine.Command(ResolveProviderIngressAction{})}, nil
}

func ingressReceivedReduce(s struct {
	Ingress ProviderResponseState
	In      ExchangeInput
}, _ IngressReceivedEvent) (ProviderResponseState, []machine.Event, []machine.Command, error) {
	if s.Ingress.Ingress == nil {
		return s.Ingress, nil, nil, canonical.InternalError("provider ingress is nil")
	}
	return s.Ingress, nil, []machine.Command{machine.Command(DecodeProviderEnvelopeAction{})}, nil
}

func envelopeDecodedReduce(s DecodedEnvelopeState, _ EnvelopeDecodedEvent) (DecodedEnvelopeState, []machine.Event, []machine.Command, error) {
	return s, nil, []machine.Command{machine.Command(CaptureContinuationAction{})}, nil
}

func continuationCapturedReduce(s DecodedEnvelopeState, _ ContinuationCapturedEvent) (DecodedEnvelopeState, []machine.Event, []machine.Command, error) {
	return s, nil, []machine.Command{machine.Command(EncodeClientOutputAction{})}, nil
}

func pipelineCompletedReduce(out pipelineOutcomeState, _ PipelineCompletedEvent) (pipelineOutcomeState, []machine.Event, []machine.Command, error) {
	return out, nil, nil, nil
}
