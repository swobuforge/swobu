package requestpath

import (
	"context"

	"github.com/metrofun/swobu/internal/domain/compatibility"
	"github.com/metrofun/swobu/internal/domain/endpointintent"
	"github.com/metrofun/swobu/internal/ports"
)

func wrapEvidenceStreamWithUsageReconciliation(
	ctx context.Context,
	sink ports.RequestEvidenceSink,
	stream compatibility.CanonicalOutputEventStream,
	requestID string,
	endpointName endpointintent.EndpointName,
	target ports.RoutableTarget,
	provenance IngressProvenance,
	attemptCount int,
	continuityRecovered bool,
	continuityRecoveryTrigger string,
	requestedModel string,
	resolvedModel string,
	resolutionMode string,
) compatibility.CanonicalOutputEventStream {
	if sink == nil || stream == nil {
		return stream
	}
	// Reconciliation is completion-gated: we emit a replacement terminal success
	// event only after observing a completed output event with non-zero usage.
	// If the stream is closed before completion, no additional reconciliation
	// event is emitted.
	return &evidenceUsageReconcilingCanonicalOutputEventStream{
		ctx:                       ctx,
		sink:                      sink,
		inner:                     stream,
		requestID:                 requestID,
		endpointName:              endpointName,
		target:                    target,
		provenance:                provenance,
		attemptCount:              attemptCount,
		continuityRecovered:       continuityRecovered,
		continuityRecoveryTrigger: continuityRecoveryTrigger,
		requestedModel:            requestedModel,
		resolvedModel:             resolvedModel,
		resolutionMode:            resolutionMode,
	}
}

type evidenceUsageReconcilingCanonicalOutputEventStream struct {
	ctx   context.Context
	sink  ports.RequestEvidenceSink
	inner compatibility.CanonicalOutputEventStream

	requestID                 string
	endpointName              endpointintent.EndpointName
	target                    ports.RoutableTarget
	provenance                IngressProvenance
	attemptCount              int
	continuityRecovered       bool
	continuityRecoveryTrigger string
	requestedModel            string
	resolvedModel             string
	resolutionMode            string
	reconciled                bool
}

func (s *evidenceUsageReconcilingCanonicalOutputEventStream) Next() (compatibility.OutputEvent, error) {
	event, err := s.inner.Next()
	if err != nil {
		return compatibility.OutputEvent{}, err
	}
	if event.Kind != compatibility.OutputEventCompleted || s.reconciled {
		return event, nil
	}
	usage := tokenUsageFromOutputEvent(event)
	if usage.IsZero() {
		return event, nil
	}
	terminal, terminalErr := newSuccessEvidenceEvent(
		s.requestID,
		s.endpointName,
		s.target,
		s.provenance,
		s.attemptCount,
		s.continuityRecovered,
		s.continuityRecoveryTrigger,
		s.requestedModel,
		s.resolvedModel,
		s.resolutionMode,
		usage,
	)
	emitEvidenceEventIfValid(s.ctx, s.sink, terminal, terminalErr)
	s.reconciled = true
	return event, nil
}

func (s *evidenceUsageReconcilingCanonicalOutputEventStream) Close() error {
	// Closing without a completed event preserves already-emitted evidence.
	// We do not synthesize usage reconciliation on close.
	return s.inner.Close()
}
