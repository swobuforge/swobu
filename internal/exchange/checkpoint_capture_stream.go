package exchange

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

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

// checkpointCaptureResponseStream records the canonical response pulled by the client
// encoder. It projects one draft at terminal success and never writes storage;
// the delivery wrapper later gates terminal publication on commit.
type checkpointCaptureResponseStream struct {
	upstream canonical.ResponseStream
	binding  canonical.ResponseBinding
	events   []canonical.Event
	mu       sync.RWMutex
	result   checkpointCaptureSnapshot
}

func newCheckpointCaptureResponseStream(upstream canonical.ResponseStream, binding canonical.ResponseBinding) *checkpointCaptureResponseStream {
	return &checkpointCaptureResponseStream{upstream: upstream, binding: binding}
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
			err := errors.New("response identity refinement does not match attempted target")
			s.fail(err)
			return canonical.Event{}, err
		}
	}
	s.events = append(s.events, event)
	status, terminal := responseTerminalStatus(event)
	if !terminal {
		return event, nil
	}
	if status != canonical.EnvelopeStatusCompleted {
		s.fail(errors.New("canonical response did not complete successfully"))
		return event, nil
	}
	closed, projectionErr := canonical.ReadClosedEnvelope(ctx, canonical.NewSliceEventReader(append([]canonical.Event(nil), s.events...)), canonical.EnvResponse)
	if projectionErr != nil {
		s.fail(fmt.Errorf("projecting checkpoint response: %w", projectionErr))
		return event, nil
	}
	response, projectionErr := closed.ProjectResponse()
	if projectionErr != nil {
		s.fail(fmt.Errorf("projecting checkpoint response: %w", projectionErr))
		return event, nil
	}
	s.mu.Lock()
	if s.result.state == checkpointCapturePending {
		s.result = checkpointCaptureSnapshot{state: checkpointCaptureCompleted, response: *response}
	}
	s.mu.Unlock()
	return event, nil
}

func (s *checkpointCaptureResponseStream) Close(ctx context.Context) error {
	if s.upstream == nil {
		return nil
	}
	return s.upstream.Close(ctx)
}

func (s *checkpointCaptureResponseStream) snapshot() checkpointCaptureSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.result
}

func (s *checkpointCaptureResponseStream) fail(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.result.state == checkpointCapturePending {
		s.result = checkpointCaptureSnapshot{state: checkpointCaptureFailed, err: err}
	}
}
