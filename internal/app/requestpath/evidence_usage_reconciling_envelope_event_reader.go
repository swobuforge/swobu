package requestpath

import (
	"context"
	"time"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	"github.com/swobuforge/swobu/internal/domain/runtimeevidence"
	"github.com/swobuforge/swobu/internal/ports"
)

func wrapEvidenceEnvelopeWithUsageReconciliation(
	ctx context.Context,
	sink ports.RequestEvidenceSink,
	stream canonical.EventReader,
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
) canonical.EventReader {
	if sink == nil || stream == nil {
		return stream
	}
	return &evidenceUsageReconcilingEnvelopeEventReader{
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

type evidenceUsageReconcilingEnvelopeEventReader struct {
	ctx   context.Context
	sink  ports.RequestEvidenceSink
	inner canonical.EventReader

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
	usage                     runtimeevidence.TokenUsage
}

func (s *evidenceUsageReconcilingEnvelopeEventReader) Next(ctx context.Context) (canonical.Event, error) {
	event, err := s.inner.Next(ctx)
	if err != nil {
		return canonical.Event{}, err
	}
	if s.firstAt.IsZero() {
		s.firstAt = time.Now()
	}
	if event.Kind == canonical.EventUsage {
		if payload, ok := event.Payload.(canonical.UsagePayload); ok {
			s.usage = runtimeevidenceFromTokenUsage(payload.Usage)
		}
	}
	if s.reconciled {
		return event, nil
	}
	if event.Kind == canonical.EventEnvelopeEnd {
		if payload, ok := event.Payload.(canonical.EnvelopeEndPayload); ok &&
			payload.Kind == canonical.EnvResponse &&
			payload.Status == canonical.EnvelopeStatusCompleted {
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
				s.usage,
			)
			emitEvidenceEventIfValid(s.ctx, s.sink, terminal, terminalErr)
			s.reconciled = true
		}
	}
	return event, nil
}

func (s *evidenceUsageReconcilingEnvelopeEventReader) Close(ctx context.Context) error {
	if s == nil || s.inner == nil {
		return nil
	}
	return s.inner.Close(ctx)
}

func runtimeevidenceFromTokenUsage(usage canonical.TokenUsage) runtimeevidence.TokenUsage {
	inputValue, hasInput := usage.InputTokens()
	outputValue, hasOutput := usage.OutputTokens()
	cacheReadValue, hasCacheRead := usage.CacheReadTokens()
	cacheWriteValue, hasCacheWrite := usage.CacheWriteTokens()

	var inputPtr *int
	if hasInput {
		inputPtr = &inputValue
	}
	var outputPtr *int
	if hasOutput {
		outputPtr = &outputValue
	}
	var cacheReadPtr *int
	if hasCacheRead {
		cacheReadPtr = &cacheReadValue
	}
	var cacheWritePtr *int
	if hasCacheWrite {
		cacheWritePtr = &cacheWriteValue
	}

	mapped, err := runtimeevidence.NewTokenUsageWithOptional(inputPtr, outputPtr, cacheReadPtr, cacheWritePtr)
	if err != nil {
		return runtimeevidence.NewUnknownTokenUsage()
	}
	return mapped
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
