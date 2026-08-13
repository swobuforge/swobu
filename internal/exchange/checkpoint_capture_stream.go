package exchange

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

type checkpointCaptureState uint8

const (
	checkpointCapturePending checkpointCaptureState = iota
	checkpointCaptureCompleted
	checkpointCaptureFailed
)

type checkpointCaptureSnapshot struct {
	state    checkpointCaptureState
	response canonical.CanonicalResponse
	err      error
}

// checkpointCaptureResponseStream records the canonical response pulled toward
// client encoding. It projects one draft at terminal success and never writes
// storage; checkpointTerminalGate owns publication gating.
//
// It folds each event incrementally into a canonical.ResponseProjector as the
// stream is read — it does NOT retain the event slice. Per-response memory
// therefore scales with completed item count, not with streamed delta count
// (epic-50 task 010). The terminal snapshot is materialized once from the
// folded state, producing the same projection the prior retain-and-reproject
// path did.
type checkpointCaptureResponseStream struct {
	upstream  canonical.ResponseStream
	binding   canonical.ResponseBinding
	projector *canonical.ResponseProjector
	result    checkpointCaptureSnapshot
}

func newCheckpointCaptureResponseStream(upstream canonical.ResponseStream, binding canonical.ResponseBinding) *checkpointCaptureResponseStream {
	return &checkpointCaptureResponseStream{upstream: upstream, binding: binding, projector: canonical.NewResponseProjector(binding)}
}

func (s *checkpointCaptureResponseStream) Next(ctx context.Context) (canonical.Event, error) {
	event, err := s.upstream.Next(ctx)
	if err != nil {
		if !errors.Is(err, io.EOF) {
			s.fail(err)
		}
		return canonical.Event{}, err
	}
	if event.Kind == canonical.EventResponseIdentity {
		payload, ok := event.Payload.(canonical.ResponseIdentityPayload)
		if !ok || payload.Response.SwobuID != s.binding.SwobuID {
			err := errors.New("response identity does not match configured checkpoint ID")
			s.fail(err)
			return canonical.Event{}, err
		}
		if payload.Response.Responses != nil && (payload.Response.Responses.TargetID != s.binding.TargetID || payload.Response.Responses.TargetVersion != s.binding.TargetVersion) {
			err := errors.New("response native handle does not match attempted target")
			s.fail(err)
			return canonical.Event{}, err
		}
		if payload.Response.Interactions != nil && (payload.Response.Interactions.TargetID != s.binding.TargetID || payload.Response.Interactions.TargetVersion != s.binding.TargetVersion) {
			err := errors.New("response native handle does not match attempted target")
			s.fail(err)
			return canonical.Event{}, err
		}
	}
	// Fold the event into projection state; the event itself is not retained.
	if foldErr := s.projector.Apply(event); foldErr != nil && s.result.state == checkpointCapturePending {
		s.fail(fmt.Errorf("projecting checkpoint response: %w", foldErr))
		return event, nil
	}
	status, terminal := responseTerminalStatus(event)
	if !terminal {
		return event, nil
	}
	if status != canonical.EnvelopeStatusCompleted {
		s.fail(errors.New("canonical response did not complete successfully"))
		return event, nil
	}
	if s.result.state == checkpointCapturePending {
		response, projectionErr := s.projector.Done()
		if projectionErr != nil {
			s.fail(fmt.Errorf("projecting checkpoint response: %w", projectionErr))
			return event, nil
		}
		s.result = checkpointCaptureSnapshot{state: checkpointCaptureCompleted, response: *response}
	}
	return event, nil
}

func (s *checkpointCaptureResponseStream) Close(ctx context.Context) error {
	if s.upstream == nil {
		return nil
	}
	return s.upstream.Close(ctx)
}

func (s *checkpointCaptureResponseStream) snapshot() checkpointCaptureSnapshot {
	return s.result
}

func (s *checkpointCaptureResponseStream) fail(err error) {
	if s.result.state == checkpointCapturePending {
		s.result = checkpointCaptureSnapshot{state: checkpointCaptureFailed, err: err}
	}
}
