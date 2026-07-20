package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

var errCheckpointCommit = errors.New("checkpoint commit failed")

// IsCheckpointCommitFailure reports whether terminal response consumption
// failed because the completed checkpoint could not be stored.
func IsCheckpointCommitFailure(err error) bool { return errors.Is(err, errCheckpointCommit) }

// CheckpointConfig configures construction of one checkpoint at terminal success.
type CheckpointConfig struct {
	// WorkspaceSlug is the validated checkpoint partition resolved from the request URL.
	WorkspaceSlug string
	// ExchangeID is the logical exchange identifier (for projection/logging only).
	ExchangeID string
	// Binding is the same identity tuple already applied by the upstream binder.
	Binding canonical.ResponseBinding
	// Store receives the built record.
	Store Store
	// Request is the complete semantic state that produced this response.
	Request canonical.CanonicalRequest
	// ResolvedMedia retains owned bytes bound to exact request occurrences.
	ResolvedMedia ResolvedMedia
	// MaxBytes bounds the estimated retained semantic state and owned media.
	MaxBytes int64
}

// checkpointStream wraps a canonical event stream and stores a checkpoint at
// terminal success. Non-terminal events stream immediately. Terminal success
// is only returned after Store.Put succeeds.
//
// IDENTITY OBSERVATION: identity is already bound before this stream. Session
// commit validates it and never rewrites client-visible or native refinement facts.
//
// FAILURE: If Store.Put fails after partial streaming, the terminal event is
// replaced with EventError followed by a synthetic EnvelopeEnd{Status: Error},
// so every downstream encoder sees a proper terminal failure.
type checkpointStream struct {
	upstream        canonical.ResponseStream
	config          CheckpointConfig
	events          []canonical.Event // all events seen so far (for projection)
	returned        int               // events already returned to downstream
	committed       bool
	startedResponse bool
	commitErr       error
}

// CheckpointStream is a response stream whose terminal checkpoint commit
// failure remains available to buffered and wire delivery adapters.
type CheckpointStream interface {
	canonical.ResponseStream
	CommitError() error
}

// NewCheckpointStream gates terminal success on checkpoint storage while
// preserving immediate delivery of non-terminal response events.
func NewCheckpointStream(upstream canonical.ResponseStream, config CheckpointConfig) CheckpointStream {
	return &checkpointStream{upstream: upstream, config: config}
}

// Validate reports whether the configuration can safely persist a checkpoint.
func (c CheckpointConfig) Validate() error {
	if c.Store == nil {
		return errors.New("checkpoint store is nil")
	}
	if strings.TrimSpace(c.WorkspaceSlug) == "" { // swobu:io-string source=domain
		return errors.New("checkpoint workspace slug is empty")
	}
	if err := (canonical.ResponseRef{SwobuID: c.Binding.SwobuID}).ValidateCommittedResponse(); err != nil {
		return fmt.Errorf("checkpoint response reference: %w", err)
	}
	if c.MaxBytes <= 0 {
		return errors.New("checkpoint size limit must be positive")
	}
	return nil
}

// Next implements canonical.ResponseStream.
func (r *checkpointStream) Next(ctx context.Context) (canonical.Event, error) {
	if r.committed && r.returned < len(r.events) {
		ev := r.events[r.returned]
		r.returned++
		return ev, nil
	}
	if r.committed {
		return canonical.Event{}, io.EOF
	}

	ev, err := r.upstream.Next(ctx)
	if errors.Is(err, io.EOF) {
		if r.startedResponse && len(r.events) > 0 {
			// A checkpoint-producing response that stops before terminal completion
			// must fail closed instead of surfacing a truncated success shape.
			return r.synthesizeTerminalFailure(
				r.events[len(r.events)-1],
				"provider_stream_incomplete",
				"provider stream ended before completed",
				errors.New("provider stream ended before completed"),
				false,
			), nil
		}
		return canonical.Event{}, io.EOF
	}
	if err != nil {
		if r.startedResponse && len(r.events) > 0 {
			// Any upstream reader failure after the response has started must
			// surface as a terminal failure, not as a transport-level abort.
			return r.synthesizeTerminalFailure(
				r.events[len(r.events)-1],
				"provider_stream_decode_failed",
				"provider stream failed after response start",
				err,
				false,
			), nil
		}
		return canonical.Event{}, err
	}

	// Track response start and validate the already-bound identity.
	if ev.Kind == canonical.EventEnvelopeStart {
		if payload, ok := ev.Payload.(canonical.EnvelopeStartPayload); ok && payload.Kind == canonical.EnvResponse {
			r.startedResponse = true
			ev.Meta.NativeID = ""
		}
	}
	if ev.Kind == canonical.EventResponseIdentity {
		payload, ok := ev.Payload.(canonical.ResponseIdentityPayload)
		if !ok || payload.Response.SwobuID != r.config.Binding.SwobuID {
			return canonical.Event{}, fmt.Errorf("response identity does not match configured checkpoint ID")
		}
		if payload.Response.Responses != nil && (payload.Response.Responses.TargetID != r.config.Binding.TargetID || payload.Response.Responses.TargetVersion != r.config.Binding.TargetVersion) {
			return canonical.Event{}, fmt.Errorf("response identity refinement does not match attempted target")
		}
	}

	r.events = append(r.events, ev)

	if status, ok := responseEnvelopeEndStatus(ev); ok {
		if status == canonical.EnvelopeStatusCompleted {
			if err := r.doCommit(ctx); err != nil {
				return r.synthesizeTerminalFailure(
					ev,
					"checkpoint_commit_failed",
					"response could not be saved for session resumption",
					err,
					true,
				), nil
			}
		}
		r.committed = true
		result := r.events[r.returned]
		r.returned++
		return result, nil
	}

	r.returned++
	return ev, nil
}

// Close implements io.Closer semantics.
func (r *checkpointStream) Close(ctx context.Context) error {
	if r.upstream == nil {
		return nil
	}
	return r.upstream.Close(ctx)
}

// CommitError returns the store error if terminal commit failed, or nil.
func (r *checkpointStream) CommitError() error { return r.commitErr }

func (r *checkpointStream) synthesizeTerminalFailure(base canonical.Event, code string, message string, err error, replaceLast bool) canonical.Event {
	if code == "checkpoint_commit_failed" {
		err = fmt.Errorf("%w: %v", errCheckpointCommit, err)
	}
	r.commitErr = err
	slog.Warn("response terminal failure",
		"component", "session",
		"event", "response_terminal_failure",
		"code", code,
		"exchange_id", base.ExchangeID,
		"response_id", r.config.Binding.SwobuID,
		"failure_origin", terminalFailureOrigin(code),
		"response_started", r.startedResponse,
		"last_event_kind", base.Kind,
		"last_event_seq", base.Seq,
		"last_env_id", base.EnvID,
		"recorded_event_count", len(r.events),
		"returned_event_count", r.returned,
		"replace_last_event", replaceLast,
		"error_type", fmt.Sprintf("%T", err),
		"error", err.Error(),
	)
	errSeq := base.Seq
	endSeq := base.Seq + 1
	if !replaceLast {
		errSeq = base.Seq + 1
		endSeq = base.Seq + 2
	}
	errEvent := canonical.Event{
		ExchangeID: base.ExchangeID,
		Seq:        errSeq,
		Time:       base.Time,
		Kind:       canonical.EventError,
		EnvID:      base.EnvID,
		ParentID:   base.ParentID,
		Payload: canonical.ErrorPayload{
			Code:    code,
			Message: message,
		},
	}
	endEvent := canonical.Event{
		ExchangeID: base.ExchangeID,
		Seq:        endSeq,
		Time:       time.Now().UTC(),
		Kind:       canonical.EventEnvelopeEnd,
		EnvID:      base.EnvID,
		ParentID:   base.ParentID,
		Payload: canonical.EnvelopeEndPayload{
			Kind:   canonical.EnvResponse,
			Status: canonical.EnvelopeStatusError,
		},
	}
	if replaceLast {
		r.events[len(r.events)-1] = errEvent
	} else {
		r.events = append(r.events, errEvent)
	}
	r.events = append(r.events, endEvent)
	r.committed = true
	result := r.events[r.returned]
	r.returned++
	return result
}

func terminalFailureOrigin(code string) string {
	if code == "provider_stream_decode_failed" {
		return "provider_stream_read_error"
	}
	if code == "provider_stream_incomplete" {
		return "provider_stream_eof_before_terminal"
	}
	if code == "checkpoint_commit_failed" {
		return "checkpoint_commit"
	}
	return "unknown"
}

func responseEnvelopeEndStatus(ev canonical.Event) (canonical.EnvelopeStatus, bool) {
	if ev.Kind != canonical.EventEnvelopeEnd {
		return "", false
	}
	payload, ok := ev.Payload.(canonical.EnvelopeEndPayload)
	if !ok {
		return "", false
	}
	if payload.Kind != canonical.EnvResponse {
		return "", false
	}
	return payload.Status, true
}

func (r *checkpointStream) doCommit(ctx context.Context) error {
	if err := r.config.Validate(); err != nil {
		return err
	}

	// Project the response from all events seen so far.
	closed, err := canonical.ReadClosedEnvelope(ctx,
		canonical.NewSliceEventReader(append([]canonical.Event(nil), r.events...)), canonical.EnvResponse)
	if err != nil {
		return fmt.Errorf("projecting response for checkpoint commit: %w", err)
	}
	output, err := closed.ProjectResponse()
	if err != nil {
		return fmt.Errorf("projecting response for checkpoint commit: %w", err)
	}

	record := Checkpoint{
		Request:       r.config.Request.Clone(),
		Response:      *output,
		ResolvedMedia: r.config.ResolvedMedia.Clone(),
		CreatedAt:     time.Now().UTC(),
	}
	if err := validateCheckpointSize(record, r.config.MaxBytes); err != nil {
		return err
	}

	return r.config.Store.Put(ctx, r.config.WorkspaceSlug, record)
}
