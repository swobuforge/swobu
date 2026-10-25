package exchange

import (
	"fmt"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// localToolClientProjection preserves one client envelope while Exchange owns
// multiple provider rounds. Message events are safe to expose before the round
// closes; locally owned tool work is not. Exact runtime bindings let caller
// tools keep their position without granting execution authority. The reducer
// must classify the completed round before
// either resuming local work or releasing withheld final items and completion.
// It stores ordinal mappings, not streamed deltas or provider execution state.
type localToolClientProjection struct {
	responseID canonical.SwobuResponseID
	envelope   canonical.EnvelopeID
	sequence   int64
	started    bool
	identified bool
	finished   bool
	nextItem   uint32
	messages   map[uint32]uint32
	// ownsTool uses frozen runtime bindings, never names. Nil withholds calls.
	ownsTool func(canonical.ToolKey) bool
}

func newLocalToolClientProjection(id canonical.SwobuResponseID) *localToolClientProjection {
	return &localToolClientProjection{responseID: id, messages: make(map[uint32]uint32)}
}

// beginRound retires provider-local ordinals without retiring client ordinals.
// Call only after the completed provider round has been validated and classified.
func (p *localToolClientProjection) beginRound() {
	p.messages = make(map[uint32]uint32)
}

// apply accepts already-validated provider events. A false result withholds an
// event from delivery, not from the round's canonical projector or MCP reducer.
func (p *localToolClientProjection) apply(event canonical.Event) (canonical.Event, bool, error) {
	if p.finished {
		return canonical.Event{}, false, fmt.Errorf("local-tool client projection is already finished")
	}
	switch event.Kind {
	case canonical.EventEnvelopeStart:
		if p.started {
			return canonical.Event{}, false, nil
		}
		p.started, p.envelope = true, event.EnvID
	case canonical.EventResponseIdentity:
		if p.identified {
			return canonical.Event{}, false, nil
		}
		p.identified = true
		// Native handles belong to round continuation, never to the aggregate
		// client identity. The final checkpoint transform retains the final handle.
		event.Payload = canonical.ResponseIdentityPayload{Response: canonical.ResponseRef{SwobuID: p.responseID}}
	case canonical.EventItemStart, canonical.EventContentStart, canonical.EventTextDelta, canonical.EventArgsDelta, canonical.EventItemCompleted:
		item, ok := event.Payload.(canonical.ItemEvent)
		if !ok {
			return canonical.Event{}, false, fmt.Errorf("local-tool client item event has invalid payload")
		}
		if event.Kind == canonical.EventItemStart {
			start, ok := item.Payload.(canonical.ItemStartPayload)
			if !ok {
				return canonical.Event{}, false, fmt.Errorf("local-tool client item start has invalid payload")
			}
			visible := start.Kind() == canonical.ItemKindMessage
			if call, ok := start.ToolCall(); ok && p.ownsTool != nil {
				visible = !p.ownsTool(call.Tool)
			}
			if visible {
				p.messages[item.Position.Item] = p.nextItem
				p.nextItem++
			}
		}
		ordinal, visible := p.messages[item.Position.Item]
		if !visible {
			return canonical.Event{}, false, nil
		}
		item.Position.Item = ordinal
		event.Payload = item
	case canonical.EventUsage, canonical.EventFinish, canonical.EventEnvelopeEnd:
		return canonical.Event{}, false, nil
	case canonical.EventError:
		// Validated provider errors are returned through the owning stream's
		// failure path, not laundered into a successful aggregate response.
		return canonical.Event{}, false, fmt.Errorf("local-tool provider round failed")
	default:
		return canonical.Event{}, false, fmt.Errorf("unsupported local-tool client event %q", event.Kind)
	}
	return p.rebase(event), true, nil
}

// finish releases only the final round's withheld items after ownership has
// been resolved. Already-delivered messages are not synthesized a second time.
// usage is the reducer's all-round total; intermediate terminals never escape.
func (p *localToolClientProjection) finish(exchangeID string, response canonical.CanonicalResponse, usage canonical.TokenUsage) ([]canonical.Event, error) {
	if !p.started || !p.identified || p.finished {
		return nil, fmt.Errorf("local-tool client projection cannot finish outside an open identified response")
	}
	var output []canonical.Event
	items := response.Items()
	withheld := make(map[uint32]uint32)
	for _, event := range canonical.SynthesizeResponseEnvelopeEvents(exchangeID, canonical.ResponseRef{SwobuID: p.responseID}, response.Model(), response.Items(), response.Completion(), usage) {
		switch event.Kind {
		case canonical.EventEnvelopeStart, canonical.EventResponseIdentity:
			continue
		case canonical.EventItemStart, canonical.EventContentStart, canonical.EventTextDelta, canonical.EventArgsDelta, canonical.EventItemCompleted:
			item := event.Payload.(canonical.ItemEvent)
			original := items[item.Position.Item]
			if call, ok := original.ToolCall(); ok && p.ownsTool != nil && p.ownsTool(call.Tool()) {
				continue
			}
			if _, delivered := p.messages[item.Position.Item]; delivered {
				continue
			}
			ordinal, assigned := withheld[item.Position.Item]
			if !assigned {
				ordinal = p.nextItem
				withheld[item.Position.Item] = ordinal
				p.nextItem++
			}
			item.Position.Item = ordinal
			event.Payload = item
		}
		output = append(output, p.rebase(event))
	}
	p.finished = true
	return output, nil
}

func (p *localToolClientProjection) rebase(event canonical.Event) canonical.Event {
	p.sequence++
	event.Seq = p.sequence
	event.EnvID, event.ParentID = p.envelope, ""
	// Provider IDs/indexes may restart each round. Client codecs derive their
	// presentation identity from the stable canonical envelope and item position.
	event.Meta.NativeID, event.Meta.NativeIndex = "", nil
	return event
}
