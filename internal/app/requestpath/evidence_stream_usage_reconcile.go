package requestpath

import (
	"context"
	"time"

	"github.com/swobuforge/swobu/internal/domain/compatibility"
	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	"github.com/swobuforge/swobu/internal/domain/runtimeevidence"
	"github.com/swobuforge/swobu/internal/ports"
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
		startedAt:                 time.Now(),
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

	startedAt time.Time
	firstAt   time.Time

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
	if s.firstAt.IsZero() {
		s.firstAt = time.Now()
	}
	if event.Kind != compatibility.OutputEventCompleted || s.reconciled {
		return event, nil
	}
	usage := tokenUsageFromOutputEvent(event)
	now := time.Now()
	timing := runtimeevidence.NewUnknownTiming()
	if !s.startedAt.IsZero() {
		durationMS := elapsedMillisAtLeastOne(s.startedAt, now)
		ttfbStart := s.firstAt
		if ttfbStart.IsZero() {
			ttfbStart = now
		}
		ttfbMS := elapsedMillisAtLeastOne(s.startedAt, ttfbStart)
		if mapped, timingErr := runtimeevidence.NewTimingWithOptional(&ttfbMS, &durationMS); timingErr == nil {
			timing = mapped
		}
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
		timing,
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

func elapsedMillisAtLeastOne(start time.Time, end time.Time) int {
	if start.IsZero() {
		return 1
	}
	if end.Before(start) {
		return 1
	}
	elapsed := int(end.Sub(start).Milliseconds())
	if elapsed < 1 {
		return 1
	}
	return elapsed
}
