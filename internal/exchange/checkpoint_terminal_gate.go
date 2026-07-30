package exchange

import (
	"context"
	"errors"
	"log/slog"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/historyfingerprint"
	"github.com/swobuforge/swobu/internal/wire"
)

// checkpointTerminalGate withholds canonical terminal success until the
// response checkpoint is addressable. Storage failure replaces completion with
// one canonical failed terminal and is then returned after those events so the
// delivery owner retains the concrete checkpoint-failure classification.
type checkpointTerminalGate struct {
	capture   *checkpointCaptureResponseStream
	codec     wire.ClientCodec
	request   canonical.CanonicalRequest
	committer *checkpointCommitter
	held      []canonical.Event
	pending   []canonical.Event
	terminal  error
	last      canonical.Event
	response  canonical.EnvelopeID
}

func newCheckpointTerminalGate(capture *checkpointCaptureResponseStream, codec wire.ClientCodec, request canonical.CanonicalRequest, committer *checkpointCommitter) *checkpointTerminalGate {
	return &checkpointTerminalGate{
		capture: capture, codec: codec, request: request.Clone(), committer: committer,
	}
}

func (g *checkpointTerminalGate) Next(ctx context.Context) (canonical.Event, error) {
	if len(g.pending) > 0 {
		event := g.pending[0]
		g.pending = g.pending[1:]
		g.last = event
		return event, nil
	}
	if g.terminal != nil {
		return canonical.Event{}, g.terminal
	}
	event, err := g.capture.Next(ctx)
	if err != nil {
		return canonical.Event{}, err
	}
	if event.Kind == canonical.EventFinish {
		g.held = append(g.held, event)
		return g.Next(ctx)
	}
	if event.Kind == canonical.EventEnvelopeStart {
		if payload, ok := event.Payload.(canonical.EnvelopeStartPayload); ok && payload.Kind == canonical.EnvResponse {
			g.response = event.EnvID
		}
	}
	status, terminal := responseTerminalStatus(event)
	if !terminal {
		g.last = event
		return event, nil
	}
	if status != canonical.EnvelopeStatusCompleted {
		g.pending = append(g.held, event)
		g.held = nil
		return g.Next(ctx)
	}

	captured := g.capture.snapshot()
	if captured.state != checkpointCaptureCompleted {
		err := errors.New("checkpoint response capture did not complete")
		if captured.err != nil {
			err = captured.err
		}
		return canonical.Event{}, checkpointCommitError(err)
	}

	var responseFingerprint = g.responseFingerprint(captured.response)
	if err := g.committer.commitDocument(ctx, captured.response, responseFingerprint); err != nil {
		base := g.last
		if base.Kind == "" {
			base = event
		}
		base.EnvID = g.response
		base.ParentID = ""
		g.pending = terminalFailureEvents(
			base,
			string(canonical.ErrorCodeInternal),
			"response finalization failed locally; partial output may have been delivered, continuation is unavailable, and retry may repeat provider work",
		)
		g.held = nil
		g.terminal = err
		return g.Next(ctx)
	}
	g.pending = append(g.held, event)
	g.held = nil
	return g.Next(ctx)
}

func (g *checkpointTerminalGate) responseFingerprint(response canonical.CanonicalResponse) *historyfingerprint.Response {
	if g.codec == nil {
		g.logFingerprintFailure()
		return nil
	}
	projected, err := g.codec.EncodeResponseDocument(g.request, response)
	if err != nil {
		g.logFingerprintFailure()
		return nil
	}
	return projected.ResponseFingerprint
}

func (g *checkpointTerminalGate) logFingerprintFailure() {
	slog.Warn("response fingerprint projection skipped",
		"component", "exchange",
		"event", "response_fingerprint_projection_failed",
		"exchange_id", g.committer.exchangeID,
		"workspace", g.committer.workspaceSlug,
	)
}

func (g *checkpointTerminalGate) Close(ctx context.Context) error {
	if g.capture == nil {
		return nil
	}
	return g.capture.Close(ctx)
}

func (g *checkpointTerminalGate) TerminalError() error { return g.terminal }

var _ canonical.ResponseStream = (*checkpointTerminalGate)(nil)
