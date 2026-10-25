package exchange

import (
	"context"
	"io"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

type localToolDeliveryEvent struct {
	event canonical.Event
	err   error
}

// localToolDelivery connects the single reducer to client pull delivery. The
// unbuffered channel applies client backpressure to provider reads and bounds
// queued deltas to zero. Closing delivery cancels the reducer and active effects.
// Only the reducer mutates projection/checkpoint state; the channel orders reads
// of terminal continuation after the final event is sent.
type localToolDelivery struct {
	ready      chan RequestOutput
	events     chan localToolDeliveryEvent
	cancel     context.CancelFunc
	projection *localToolClientProjection
	opening    []canonical.Event
	response   ClientResponse
	checkpoint canonical.CanonicalResponse
	committer  *checkpointCommitter
	changes    []compat.Change
}

func newLocalToolDelivery(cancel context.CancelFunc) *localToolDelivery {
	return &localToolDelivery{ready: make(chan RequestOutput, 1), events: make(chan localToolDeliveryEvent), cancel: cancel}
}

func (d *localToolDelivery) Next(ctx context.Context) (canonical.Event, error) {
	select {
	case result, ok := <-d.events:
		if !ok {
			return canonical.Event{}, io.EOF
		}
		return result.event, result.err
	case <-ctx.Done():
		return canonical.Event{}, ctx.Err()
	}
}

func (d *localToolDelivery) Close(context.Context) error {
	d.cancel()
	return nil
}

func (d *localToolDelivery) send(ctx context.Context, event canonical.Event) error {
	select {
	case d.events <- localToolDeliveryEvent{event: event}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// expose keeps response opening private until message evidence or a classified
// final response exists. Tool-only rounds can therefore fail before HTTP 200.
func (d *localToolDelivery) expose(ctx context.Context, call providerCall, runner runtimeBundle, event canonical.Event) error {
	if d.response == nil {
		binding := canonical.ResponseBinding{SwobuID: d.projection.responseID, TargetID: call.backend.Target.TargetID, TargetVersion: call.backend.Target.TargetVersion}
		capture := newCheckpointCaptureResponseStream(newTerminalResponseStream(d), binding, func(canonical.CanonicalResponse) (canonical.CanonicalResponse, error) {
			return d.checkpoint.Clone(), nil
		})
		d.committer = &checkpointCommitter{exchangeID: call.exchangeID, workspaceSlug: call.workspaceSlug, store: runner.CheckpointStore, request: call.fullRequest.Clone(), advance: call.advance, executionAffinity: call.executionAffinity, resolvedFrom: call.resolvedFrom}
		gated := newCheckpointTerminalGate(capture, call.clientCodec, call.fullRequest, d.committer)
		response, err := encodeClientOutput(ctx, call, gated, true)
		if err != nil {
			return err
		}
		d.response = response
		if completion := responseCompletion(response); completion != nil {
			completion.ConfigureCompatibility(d.changes, nil)
		}
		d.ready <- RequestOutput{Response: response, Target: call.backend.Target, Compatibility: responseCompletion(response), AttemptCount: call.providerRound + 1}
		for _, opening := range d.opening {
			if err := d.send(ctx, opening); err != nil {
				return err
			}
		}
		d.opening = nil
	}
	return d.send(ctx, event)
}

func (d *localToolDelivery) apply(ctx context.Context, call providerCall, runner runtimeBundle, event canonical.Event) error {
	projected, visible, err := d.projection.apply(event)
	if err != nil || !visible {
		return err
	}
	if projected.Kind == canonical.EventEnvelopeStart || projected.Kind == canonical.EventResponseIdentity {
		d.opening = append(d.opening, projected)
		return nil
	}
	return d.expose(ctx, call, runner, projected)
}

func (d *localToolDelivery) finish(ctx context.Context, call providerCall, runner runtimeBundle, response completedProviderResponse) (ClientResponse, error) {
	d.checkpoint = response.continuation.Clone()
	if d.committer != nil {
		// Early delivery may have started before local calls. Publish the final
		// prepared request so the checkpoint includes every causal call/result.
		d.committer.request = call.fullRequest.Clone()
	}
	terminal, err := d.projection.finish(call.exchangeID, response.client, response.client.Usage())
	if err != nil {
		return nil, err
	}
	for _, event := range terminal {
		if err := d.expose(ctx, call, runner, event); err != nil {
			return nil, err
		}
	}
	return d.response, nil
}
