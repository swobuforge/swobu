// Continuation state machine near its narrow store contract.
package canonical

import (
	"context"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

// ContinuationStore is the narrow persistence contract requestpath needs for
// truthful responses-style continuation handling.
type ContinuationStore interface {
	Load(ctx context.Context, previousResponseID string) (ContinuitySnapshot, bool, error)
	MatchPrefix(ctx context.Context, namespace ContinuationNamespace, thread []CanonicalItem) (ContinuationPrefixMatch, bool, error)
	Store(ctx context.Context, namespace ContinuationNamespace, snapshot ContinuitySnapshot) error
}

// ContinuationRuntime owns continuation-state load, materialization, and
// capture while keeping canonical request values pure and I/O explicit.
type ContinuationRuntime struct {
	store ContinuationStore
}

func NewContinuationRuntime(store ContinuationStore) ContinuationRuntime {
	return ContinuationRuntime{store: store}
}

// PrepareRequest enriches one canonical request with the replay information the
// selected target protocol may need. It is the only place where continuation
// I/O is allowed to influence request preparation; the canonical request values
// themselves remain pure semantic data.
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
	previousResponseID, ok, err := PreviousResponseIDFromRequest(request)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	if m.store == nil {
		return nil, BadRequest("responses previous_response_id could not be rehydrated")
	}
	snapshot, ok, err := m.store.Load(ctx, previousResponseID)
	if err != nil {
		return nil, InternalError("response continuity state could not be loaded")
	}
	if !ok {
		return nil, BadRequest("responses previous_response_id could not be rehydrated")
	}
	cloned := snapshot.Clone()
	return &cloned, nil
}

func (m ContinuationRuntime) prepareResponseRequest(ctx context.Context, request CanonicalRequest) (CanonicalRequest, error) {
	if err := ValidateResponseContinuationSelectors(request); err != nil {
		return CanonicalRequest{}, err
	}
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
	anchor := snapshot.Thread
	prefixLen := longestCommonPrefixLength(anchor, currentThread)

	var thread []CanonicalItem
	preparedPreviousResponseID := previousResponseID

	switch {
	case len(currentThread) == 0:
		// Parent-only continuation is still meaningful on responses surfaces. The
		// anchor thread becomes authoritative until the client contributes a new turn.
		thread = cloneCanonicalItems(anchor)
	case prefixLen == len(anchor):
		// Native parent optimization is valid only when the resolved anchor is a
		// full prefix of the authored thread; the suffix then becomes the last turn.
		thread = cloneCanonicalItems(currentThread)
	case prefixLen == 0:
		// Some clients send only the new turn when they rely on prior-thread
		// state. In that case the canonical thread is the anchored history plus
		// the new suffix, and the same derived last turn can later drive
		// truthful responses realization.
		thread = append(cloneCanonicalItems(anchor), cloneCanonicalItems(currentThread)...)
	default:
		// Partial overlap means the client rewrote history relative to the anchor.
		// We keep the authored thread as truth and drop native-parent optimization.
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

// WrapEnvelopeStream delays continuity capture until a completed response
// envelope closes. This keeps buffered and streaming continuity semantics
// equivalent while using canonical envelope events as truth.
func (m ContinuationRuntime) WrapEnvelopeStream(
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
