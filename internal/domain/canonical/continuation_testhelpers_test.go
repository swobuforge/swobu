package canonical

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

type ContinuationStore interface {
	Load(ctx context.Context, previousResponseID string) (ContinuitySnapshot, bool, error)
	MatchPrefix(ctx context.Context, namespace ContinuationNamespace, thread []CanonicalItem) (ContinuationPrefixMatch, bool, error)
	Store(ctx context.Context, namespace ContinuationNamespace, snapshot ContinuitySnapshot) error
}

func NewContinuationNamespace(raw string) ContinuationNamespace {
	return ContinuationNamespace(strings.TrimSpace(raw)) // swobu:io-string source=domain
}

type ContinuationRuntime struct {
	store ContinuationStore
}

func NewContinuationRuntime(store ContinuationStore) ContinuationRuntime {
	return ContinuationRuntime{store: store}
}

func (m ContinuationRuntime) PrepareRequest(ctx context.Context, namespace ContinuationNamespace, targetProtocol protocolkind.ProtocolKind, request CanonicalRequest) (CanonicalRequest, error) {
	if strings.TrimSpace(request.PreviousResponseID()) != "" { // swobu:io-string source=domain
		return m.prepareResponseRequest(ctx, request)
	}
	if targetProtocol == protocolkind.Responses {
		return m.prepareConversationRequest(ctx, namespace, targetProtocol, request)
	}
	return CloneCanonicalRequest(request), nil
}

func (m ContinuationRuntime) loadSnapshot(ctx context.Context, request CanonicalRequest) (*ContinuitySnapshot, error) {
	previousResponseID, ok := ContinuationSelectorFromRequest(request)
	if !ok {
		return nil, nil
	}
	if m.store == nil {
		return nil, nil
	}
	snapshot, ok, err := m.store.Load(ctx, previousResponseID)
	if err != nil {
		return nil, InternalError("response continuity state could not be loaded")
	}
	if !ok {
		return nil, BadRequest("continuation selector could not be rehydrated")
	}
	cloned := snapshot.Clone()
	return &cloned, nil
}

func (m ContinuationRuntime) prepareResponseRequest(ctx context.Context, request CanonicalRequest) (CanonicalRequest, error) {
	previousResponseID := request.PreviousResponseID()
	hasParent := strings.TrimSpace(previousResponseID) != "" // swobu:io-string source=domain
	currentThread := request.Items()
	if !hasParent {
		return NewCanonicalRequest(RequestParams{
			Model:       request.Model(),
			Items:       currentThread,
			ToolMode:    request.ToolMode(),
			CacheIntent: request.CacheIntent(),
		}), nil
	}

	snapshot, err := m.loadSnapshot(ctx, request)
	if err != nil {
		return CanonicalRequest{}, err
	}
	if snapshot == nil {
		return CanonicalRequest{}, BadRequest("continuation selector could not be rehydrated")
	}
	anchor := snapshot.Thread
	prefixLen := longestCommonPrefixLength(anchor, currentThread)

	var thread []CanonicalItem
	preparedPreviousResponseID := previousResponseID

	switch {
	case len(currentThread) == 0:
		thread = cloneCanonicalItems(anchor)
	case prefixLen == len(anchor):
		thread = cloneCanonicalItems(currentThread)
	case prefixLen == 0:
		thread = append(cloneCanonicalItems(anchor), cloneCanonicalItems(currentThread)...)
	default:
		thread = cloneCanonicalItems(currentThread)
		preparedPreviousResponseID = ""
	}

	return NewCanonicalRequest(RequestParams{
		Model:              request.Model(),
		Items:              thread,
		PreviousResponseID: preparedPreviousResponseID,
		ToolMode:           request.ToolMode(),
		CacheIntent:        request.CacheIntent(),
	}), nil
}

func (m ContinuationRuntime) prepareConversationRequest(
	_ context.Context,
	_ ContinuationNamespace,
	targetProtocol protocolkind.ProtocolKind,
	request CanonicalRequest,
) (CanonicalRequest, error) {
	if targetProtocol != protocolkind.Responses {
		return CloneCanonicalRequest(request), nil
	}
	thread := request.Items()
	return NewCanonicalRequest(RequestParams{
		Model:       request.Model(),
		Items:       thread,
		CacheIntent: request.CacheIntent(),
	}), nil
}

func (m ContinuationRuntime) WrapResponseEnvelope(
	ctx context.Context,
	namespace ContinuationNamespace,
	request CanonicalRequest,
	stream EventReader,
) (EventReader, error) {
	if m.store == nil || namespace.IsZero() || stream == nil {
		return stream, nil
	}
	thread, ok, err := ContinuationConversation(request)
	if err != nil {
		return nil, err
	}
	if !ok {
		return stream, nil
	}
	return &continuationCapturingEnvelopeEventReader{
		ctx:       ctx,
		store:     m.store,
		namespace: namespace,
		inner:     stream,
		index:     NewEnvelopeIndex(),
		thread:    thread,
	}, nil
}

type continuationCapturingEnvelopeEventReader struct {
	ctx       context.Context
	store     ContinuationStore
	namespace ContinuationNamespace
	inner     EventReader
	index     *EnvelopeIndex
	thread    []CanonicalItem
	stored    bool
}

func (r *continuationCapturingEnvelopeEventReader) Next(ctx context.Context) (Event, error) {
	ev, err := r.inner.Next(ctx)
	if err != nil {
		return Event{}, err
	}
	if observeErr := r.index.Observe(ev); observeErr != nil {
		return Event{}, InternalError("canonical envelope stream could not be assembled for continuation capture")
	}
	if ev.Kind == EventEnvelopeEnd && !r.stored {
		payload, ok := ev.Payload.(EnvelopeEndPayload)
		if ok && payload.Kind == EnvResponse && payload.Status == EnvelopeStatusCompleted {
			output, projectErr := r.index.ProjectResponse(ev.EnvID)
			if projectErr != nil {
				return Event{}, InternalError("canonical envelope response could not be projected for continuation capture")
			}
			snapshot, ok, buildErr := BuildContinuitySnapshot(r.thread, *output)
			if buildErr != nil {
				return Event{}, buildErr
			}
			if ok {
				if storeErr := r.store.Store(r.ctx, r.namespace, snapshot); storeErr != nil {
					return Event{}, InternalError("response continuity state could not be stored")
				}
			}
			r.stored = true
		}
	}
	return ev, nil
}

func (r *continuationCapturingEnvelopeEventReader) Close(ctx context.Context) error {
	if r == nil || r.inner == nil {
		return nil
	}
	return r.inner.Close(ctx)
}

func ContinuationSelectorFromRequest(request CanonicalRequest) (string, bool) {
	value := strings.TrimSpace(request.PreviousResponseID()) // swobu:io-string source=domain
	if value == "" {
		return "", false
	}
	return value, true
}

func ContinuationConversation(request CanonicalRequest) ([]CanonicalItem, bool, error) {
	items := request.Items()
	return items, len(items) > 0, nil
}

func BuildContinuitySnapshot(
	thread []CanonicalItem,
	output CanonicalOutput,
) (ContinuitySnapshot, bool, error) {
	if output == nil || output.ResultID() == "" || len(thread) == 0 {
		return ContinuitySnapshot{}, false, nil
	}
	items := output.Items()
	if len(items) == 0 {
		return ContinuitySnapshot{}, false, nil
	}
	for _, item := range items {
		switch item.Kind {
		case ItemKindText, ItemKindToolUse:
		default:
			return ContinuitySnapshot{}, false, UnsupportedOperation("canonical output item is not replayable in continuity state")
		}
	}
	return NewContinuitySnapshot(
		output.ResultID(),
		output.Model(),
		append(cloneCanonicalItems(thread), cloneCanonicalItems(items)...),
	), true, nil
}

func longestCommonPrefixLength(left []CanonicalItem, right []CanonicalItem) int {
	limit := len(left)
	if len(right) < limit {
		limit = len(right)
	}
	for i := 0; i < limit; i++ {
		if !reflect.DeepEqual(left[i], right[i]) {
			return i
		}
	}
	return limit
}

type OpenEnvelope struct {
	ID     EnvelopeID
	Kind   EnvelopeKind
	Parent EnvelopeID
	Events []Event
}

type EnvelopeIndex struct {
	open   map[EnvelopeID]*OpenEnvelope
	closed map[EnvelopeID]*ClosedEnvelope
}

func NewEnvelopeIndex() *EnvelopeIndex {
	return &EnvelopeIndex{
		open:   map[EnvelopeID]*OpenEnvelope{},
		closed: map[EnvelopeID]*ClosedEnvelope{},
	}
}

func (i *EnvelopeIndex) Observe(ev Event) error {
	switch ev.Kind {
	case EventEnvelopeStart:
		payload, ok := ev.Payload.(EnvelopeStartPayload)
		if !ok {
			return fmt.Errorf("envelope.start payload type %T is unsupported", ev.Payload)
		}
		i.appendToAncestors(ev.ParentID, ev)
		i.open[ev.EnvID] = &OpenEnvelope{ID: ev.EnvID, Kind: payload.Kind, Parent: ev.ParentID, Events: []Event{ev}}
	case EventEnvelopeEnd:
		payload, ok := ev.Payload.(EnvelopeEndPayload)
		if !ok {
			return fmt.Errorf("envelope.end payload type %T is unsupported", ev.Payload)
		}
		i.appendToAncestors(ev.EnvID, ev)
		open, ok := i.open[ev.EnvID]
		if !ok {
			return fmt.Errorf("closing unknown envelope %q", ev.EnvID)
		}
		delete(i.open, ev.EnvID)
		i.closed[ev.EnvID] = &ClosedEnvelope{ID: ev.EnvID, Kind: payload.Kind, Events: append([]Event(nil), open.Events...)}
	default:
		i.appendToAncestors(ev.EnvID, ev)
	}
	return nil
}

func (i *EnvelopeIndex) appendToAncestors(start EnvelopeID, ev Event) {
	cur := start
	for cur != "" {
		open, ok := i.open[cur]
		if !ok {
			return
		}
		open.Events = append(open.Events, ev)
		cur = open.Parent
	}
}

func (i *EnvelopeIndex) Closed(id EnvelopeID) (*ClosedEnvelope, bool) {
	out, ok := i.closed[id]
	return out, ok
}

func (i *EnvelopeIndex) ProjectResponse(id EnvelopeID) (*CanonicalOutputData, error) {
	closed, ok := i.closed[id]
	if !ok {
		return nil, fmt.Errorf("response envelope %q is not closed", id)
	}
	return closed.ProjectResponse()
}

type envelopeValidationState struct {
	kind   EnvelopeKind
	parent EnvelopeID
	closed bool
}

type GrammarValidator struct {
	open        map[EnvelopeID]envelopeValidationState
	lastSeqByEx map[string]int64
}

func NewGrammarValidator() *GrammarValidator {
	return &GrammarValidator{
		open:        map[EnvelopeID]envelopeValidationState{},
		lastSeqByEx: map[string]int64{},
	}
}

func (v *GrammarValidator) Observe(ev Event) error {
	if last, ok := v.lastSeqByEx[ev.ExchangeID]; ok && ev.Seq <= last {
		return fmt.Errorf("event sequence must be monotonic, got %d after %d", ev.Seq, last)
	}
	v.lastSeqByEx[ev.ExchangeID] = ev.Seq

	switch ev.Kind {
	case EventEnvelopeStart:
		payload, ok := ev.Payload.(EnvelopeStartPayload)
		if !ok {
			return fmt.Errorf("envelope.start payload type %T is unsupported", ev.Payload)
		}
		if _, exists := v.open[ev.EnvID]; exists {
			return fmt.Errorf("envelope %q started twice", ev.EnvID)
		}
		if ev.ParentID != "" {
			parent, ok := v.open[ev.ParentID]
			if !ok || parent.closed {
				return fmt.Errorf("envelope %q parent %q is not open", ev.EnvID, ev.ParentID)
			}
		}
		v.open[ev.EnvID] = envelopeValidationState{kind: payload.Kind, parent: ev.ParentID}
		return nil
	case EventEnvelopeEnd:
		payload, ok := ev.Payload.(EnvelopeEndPayload)
		if !ok {
			return fmt.Errorf("envelope.end payload type %T is unsupported", ev.Payload)
		}
		state, ok := v.open[ev.EnvID]
		if !ok {
			return fmt.Errorf("end references unknown envelope %q", ev.EnvID)
		}
		if state.kind != payload.Kind {
			return fmt.Errorf("envelope %q kind mismatch: have %q end %q", ev.EnvID, state.kind, payload.Kind)
		}
		for id, other := range v.open {
			if id == ev.EnvID {
				continue
			}
			if other.parent == ev.EnvID && !other.closed {
				return fmt.Errorf("parent envelope %q cannot close before child %q", ev.EnvID, id)
			}
		}
		delete(v.open, ev.EnvID)
		return nil
	case EventTextDelta, EventArgsDelta, EventUsage, EventFinish, EventError, EventMetadata:
		if ev.EnvID == "" {
			return fmt.Errorf("event %q is missing env id", ev.Kind)
		}
		if _, ok := v.open[ev.EnvID]; !ok {
			return fmt.Errorf("event %q references unknown or closed envelope %q", ev.Kind, ev.EnvID)
		}
		return nil
	default:
		return fmt.Errorf("event kind %q is unsupported", ev.Kind)
	}
}

func SynthesizeRequestFromCanonicalRequest(exchangeID string, req CanonicalRequest) ([]Event, error) {
	seq := int64(0)
	next := func() int64 {
		seq++
		return seq
	}
	requestID := EnvelopeID(fmt.Sprintf("%s:request:0", exchangeID))
	meta := map[string]string{
		"model": stringRequestModel(req),
	}
	meta["semantic_kind"] = string(req.SemanticKind())
	events := []Event{
		{
			ExchangeID: exchangeID,
			Seq:        next(),
			Time:       time.Now().UTC(),
			Kind:       EventEnvelopeStart,
			EnvID:      requestID,
			Payload: EnvelopeStartPayload{
				Kind: EnvRequest,
			},
		},
		{
			ExchangeID: exchangeID,
			Seq:        next(),
			Time:       time.Now().UTC(),
			Kind:       EventMetadata,
			EnvID:      requestID,
			Payload:    MetadataPayload{Values: meta},
		},
	}
	items := canonicalRequestItems(req)
	msgIdx := 0
	toolResultIdx := 0
	for _, item := range items {
		switch item.Kind {
		case ItemKindText:
			id := EnvelopeID(fmt.Sprintf("%s:message:%d", requestID, msgIdx))
			msgIdx++
			events = append(events,
				Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventEnvelopeStart, EnvID: id, ParentID: requestID, Payload: EnvelopeStartPayload{Kind: EnvMessage, Role: item.Author}},
				Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventTextDelta, EnvID: id, ParentID: requestID, Payload: TextDeltaPayload{Text: item.Text}},
				Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventEnvelopeEnd, EnvID: id, ParentID: requestID, Payload: EnvelopeEndPayload{Kind: EnvMessage, Status: EnvelopeStatusCompleted}},
			)
		case ItemKindToolResult:
			id := EnvelopeID(fmt.Sprintf("%s:tool_result:%d", requestID, toolResultIdx))
			toolResultIdx++
			events = append(events,
				Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventEnvelopeStart, EnvID: id, ParentID: requestID, Payload: EnvelopeStartPayload{Kind: EnvToolResult, Role: item.Author, ToolUseID: item.ToolUseID}},
				Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventTextDelta, EnvID: id, ParentID: requestID, Payload: TextDeltaPayload{Text: item.Text}},
				Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventEnvelopeEnd, EnvID: id, ParentID: requestID, Payload: EnvelopeEndPayload{Kind: EnvToolResult, Status: EnvelopeStatusCompleted}},
			)
		case ItemKindToolUse:
			id := EnvelopeID(fmt.Sprintf("%s:tool_result:%d", requestID, toolResultIdx))
			toolResultIdx++
			args := item.Input.RawObject()
			events = append(events,
				Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventEnvelopeStart, EnvID: id, ParentID: requestID, Payload: EnvelopeStartPayload{Kind: EnvToolResult, Role: item.Author, ToolUseID: item.ToolUseID, Name: item.Name}},
				Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventArgsDelta, EnvID: id, ParentID: requestID, Payload: ArgsDeltaPayload{Args: args}},
				Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventEnvelopeEnd, EnvID: id, ParentID: requestID, Payload: EnvelopeEndPayload{Kind: EnvToolResult, Status: EnvelopeStatusCompleted}},
			)
		default:
		}
	}
	events = append(events,
		Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventEnvelopeEnd, EnvID: requestID, Payload: EnvelopeEndPayload{Kind: EnvRequest, Status: EnvelopeStatusCompleted}},
	)
	return events, nil
}

func canonicalRequestItems(req CanonicalRequest) []CanonicalItem {
	return req.Items()
}

func stringRequestModel(req CanonicalRequest) string {
	return req.Model()
}
