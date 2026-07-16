package replay

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/effect"
)

// TerminalCommitConfig configures how the CommitReader builds and stores a
// replay record at terminal success.
type TerminalCommitConfig struct {
	// Scope is the replay storage partition for this record.
	Scope Scope
	// ExchangeID is the logical exchange identifier (for projection/logging only).
	ExchangeID string
	// ResponseID is the Swobu client-visible response ID allocated early.
	ResponseID ResponseID
	// Store receives the built record.
	Store Store
	// NativeReplay is the native provider pointer supplied by exchange
	// preparation. When present, CaptureRequest materializes the full semantic
	// request from the previous record plus the current delta.
	NativeReplay *NativeRef
	// CaptureRequest is the request seeded into the exchange (delta when native
	// replay is present, full otherwise). CommitReader calls CaptureRequest at
	// commit time to materialize the full persisted request.
	CaptureRequest canonical.CanonicalRequest
	// NativeExtractor is the ONLY path to capture native replay. If nil, no
	// native replay is stored. The callback receives the original provider
	// result ID and must return a fully populated NativeRef (all fields set).
	NativeExtractor func(providerResultID string, replayID ID) *NativeRef
}

// CommitReader wraps a canonical event stream and stores a replay record at
// terminal success. Non-terminal events stream immediately. Terminal success
// is only returned after Store.Put succeeds.
//
// IDENTITY REWRITE: the response envelope start is tagged with the allocated
// Swobu ResponseID via Meta.ResultID so response.created can surface it
// immediately. Provider NativeID is consumed for native replay capture, then
// cleared before downstream projection so client-visible identity comes only
// from Meta.ResultID.
//
// FAILURE: If Store.Put fails after partial streaming, the terminal event is
// replaced with EventError followed by a synthetic EnvelopeEnd{Status: Error},
// so every downstream encoder sees a proper terminal failure.
type CommitReader struct {
	upstream         canonical.EventReader
	config           TerminalCommitConfig
	events           []canonical.Event // all events seen so far (for projection)
	returned         int               // events already returned to downstream
	committed        bool
	startedResponse  bool
	providerResultID string // original provider ID, preserved for native capture
	commitErr        error
}

// NewCommitReader wraps upstream so that terminal success is gated on replay
// store commit.
func NewCommitReader(upstream canonical.EventReader, config TerminalCommitConfig) *CommitReader {
	return &CommitReader{upstream: upstream, config: config}
}

// Validate reports whether the commit configuration can safely persist a
// replay record.
func (c TerminalCommitConfig) Validate() error {
	if c.Store == nil {
		return errors.New("replay commit store is nil")
	}
	if strings.TrimSpace(c.Scope.Namespace) == "" {
		return errors.New("replay commit scope namespace is empty")
	}
	if strings.TrimSpace(c.Scope.CallerKey) == "" {
		return errors.New("replay commit scope caller key is empty")
	}
	if strings.TrimSpace(string(c.ResponseID)) == "" {
		return errors.New("replay commit response id is empty")
	}
	return nil
}

// Next implements canonical.EventReader.
func (r *CommitReader) Next(ctx context.Context) (canonical.Event, error) {
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
			// A replay-addressed response that stops before terminal completion
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

	// Rewrite streaming identity: provider result_id must not leak to the
	// client. Record the original for native replay capture; downstream sees
	// the Swobu ID only.
	if ev.Kind == canonical.EventEnvelopeStart {
		if payload, ok := ev.Payload.(canonical.EnvelopeStartPayload); ok && payload.Kind == canonical.EnvResponse {
			r.startedResponse = true
			if r.providerResultID == "" && strings.TrimSpace(ev.Meta.NativeID) != "" {
				r.providerResultID = strings.TrimSpace(ev.Meta.NativeID)
			}
			ev.Meta.NativeID = ""
			ev.Meta.ResultID = string(r.config.ResponseID)
		}
	}
	if ev.Kind == canonical.EventMetadata {
		if payload, ok := ev.Payload.(canonical.MetadataPayload); ok {
			if id := payload.Values["result_id"]; id != "" {
				if r.providerResultID == "" {
					r.providerResultID = id
				}
				if r.config.ResponseID != "" {
					mutated := make(map[string]string, len(payload.Values))
					for k, v := range payload.Values {
						mutated[k] = v
					}
					mutated["result_id"] = string(r.config.ResponseID)
					ev.Payload = canonical.MetadataPayload{Values: mutated}
					ev.Meta.NativeID = ""
					ev.Meta.ResultID = string(r.config.ResponseID)
				}
			}
		}
	}

	r.events = append(r.events, ev)

	if status, ok := responseEnvelopeEndStatus(ev); ok {
		if status == canonical.EnvelopeStatusCompleted {
			if err := r.doCommit(ctx); err != nil {
				return r.synthesizeTerminalFailure(
					ev,
					"replay_capture_failed",
					"response could not be captured for replay",
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
func (r *CommitReader) Close(ctx context.Context) error {
	if r.upstream == nil {
		return nil
	}
	return r.upstream.Close(ctx)
}

// Effects surfaces any effects accumulated by the upstream reader.
func (r *CommitReader) Effects() []effect.Effect {
	type effectReader interface {
		Effects() []effect.Effect
	}
	if er, ok := r.upstream.(effectReader); ok {
		return er.Effects()
	}
	return nil
}

// CommitError returns the store error if terminal commit failed, or nil.
func (r *CommitReader) CommitError() error { return r.commitErr }

func (r *CommitReader) synthesizeTerminalFailure(base canonical.Event, code string, message string, err error, replaceLast bool) canonical.Event {
	r.commitErr = err
	slog.Warn("replay terminal failure",
		"component", "replay",
		"code", code,
		"exchange_id", base.ExchangeID,
		"response_id", r.config.ResponseID,
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

func (r *CommitReader) doCommit(ctx context.Context) error {
	if err := r.config.Validate(); err != nil {
		return err
	}

	// Project the response from all events seen so far.
	closed, err := canonical.ReadClosedEnvelope(ctx,
		canonical.NewSliceEventReader(append([]canonical.Event(nil), r.events...)), canonical.EnvResponse)
	if err != nil {
		return fmt.Errorf("projecting response for replay commit: %w", err)
	}
	output, err := closed.ProjectResponse()
	if err != nil {
		return fmt.Errorf("projecting response for replay commit: %w", err)
	}

	// Native replay capture: only through the opt-in extractor.
	// No fallback — if the codec did not opt in, native replay is nil.
	var nativeRef *NativeRef
	if r.config.NativeExtractor != nil && r.providerResultID != "" {
		nativeRef = r.config.NativeExtractor(r.providerResultID, ReplayIDFromResponseID(r.config.ResponseID))
	}

	// Substitute Swobu response ID into the projected output for storage.
	if r.config.ResponseID != "" {
		p := output.WithResultID(string(r.config.ResponseID))
		output = &p
	}

	// Build the persisted request. CaptureRequest materializes full history
	// when a native replay pointer is present, ensuring stored records are
	// always complete semantic state.
	request, err := CaptureRequest(ctx, r.config.Store, r.config.Scope, r.config.NativeReplay, r.config.CaptureRequest)
	if err != nil {
		return fmt.Errorf("capturing request for replay: %w", err)
	}

	record := Record{
		ID:        ReplayIDFromResponseID(r.config.ResponseID),
		Scope:     r.config.Scope,
		Request:   request,
		Response:  *output,
		Native:    nativeRef,
		CreatedAt: time.Now().UTC(),
	}

	return r.config.Store.Put(ctx, r.config.Scope, record)
}
