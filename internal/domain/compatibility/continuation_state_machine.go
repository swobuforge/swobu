// Continuation state machine near its narrow store contract.
package compatibility

import (
	"context"
	"errors"
	"io"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/protocolsurface"
)

// ContinuationStore is the narrow persistence contract compatibility needs for
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
func (m ContinuationRuntime) PrepareRequest(ctx context.Context, namespace ContinuationNamespace, targetProtocol protocolsurface.Kind, request CanonicalRequest) (CanonicalRequest, error) {
	switch typed := request.(type) {
	case GenerationCanonicalRequest:
		return m.prepareResponseRequest(ctx, typed)
	case DialogCanonicalRequest:
		return m.prepareConversationRequest(ctx, namespace, targetProtocol, typed)
	default:
		return CloneCanonicalRequest(request), nil
	}
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

func (m ContinuationRuntime) prepareResponseRequest(ctx context.Context, request GenerationCanonicalRequest) (CanonicalRequest, error) {
	if err := ValidateResponseContinuationSelectors(request); err != nil {
		return nil, err
	}
	previousResponseID := request.PreviousResponseID()
	hasParent := strings.TrimSpace(previousResponseID) != ""
	currentThread := request.Thread()
	currentLastTurn := request.LastTurn()
	if !hasParent {
		if len(currentLastTurn) == 0 {
			currentLastTurn = currentThread
		}
		return NewGenerationRequest(GenerationRequestParams{
			Model:                request.Model(),
			Thread:               currentThread,
			LastTurn:             currentLastTurn,
			ToolMode:             request.ToolMode(),
			PromptCacheKey:       request.PromptCacheKey(),
			PromptCacheRetention: request.PromptCacheRetention(),
		}), nil
	}

	snapshot, err := m.loadSnapshot(ctx, request)
	if err != nil {
		return nil, err
	}
	anchor := snapshot.Thread
	prefixLen := longestCommonPrefixLength(anchor, currentThread)

	var thread []CanonicalItem
	var lastTurn []CanonicalItem
	preparedPreviousResponseID := previousResponseID

	switch {
	case len(currentThread) == 0:
		// Parent-only continuation is still meaningful on responses surfaces. The
		// anchor thread becomes authoritative until the client contributes a new turn.
		thread = cloneCanonicalItems(anchor)
		lastTurn = nil
	case prefixLen == len(anchor):
		// Native parent optimization is valid only when the resolved anchor is a
		// full prefix of the authored thread; the suffix then becomes the last turn.
		thread = cloneCanonicalItems(currentThread)
		lastTurn = cloneCanonicalItems(currentThread[prefixLen:])
	case prefixLen == 0:
		// Some clients send only the new turn when they rely on prior-thread
		// state. In that case the canonical thread is the anchored history plus
		// the new suffix, and the same derived last turn can later drive
		// truthful responses realization.
		thread = append(cloneCanonicalItems(anchor), cloneCanonicalItems(currentThread)...)
		lastTurn = cloneCanonicalItems(currentThread)
	default:
		// Partial overlap means the client rewrote history relative to the anchor.
		// We keep the authored thread as truth and drop native-parent optimization.
		thread = cloneCanonicalItems(currentThread)
		lastTurn = cloneCanonicalItems(currentThread)
		preparedPreviousResponseID = ""
	}

	return NewGenerationRequest(GenerationRequestParams{
		Model:                request.Model(),
		Thread:               thread,
		LastTurn:             lastTurn,
		PreviousResponseID:   preparedPreviousResponseID,
		ToolMode:             request.ToolMode(),
		PromptCacheKey:       request.PromptCacheKey(),
		PromptCacheRetention: request.PromptCacheRetention(),
	}), nil
}

func (m ContinuationRuntime) prepareConversationRequest(
	ctx context.Context,
	namespace ContinuationNamespace,
	targetProtocol protocolsurface.Kind,
	request DialogCanonicalRequest,
) (CanonicalRequest, error) {
	if targetProtocol != protocolsurface.Responses {
		return CloneCanonicalRequest(request), nil
	}

	thread := request.Items()
	lastTurn := thread

	if m.store != nil && !namespace.IsZero() {
		match, ok, err := m.store.MatchPrefix(ctx, namespace, thread)
		if err != nil {
			return nil, InternalError("response continuity state could not be loaded")
		}
		if ok {
			anchor := match.Snapshot.Thread
			if match.PrefixLength == len(anchor) {
				// Any best-match anchor whose full thread is a prefix is good enough
				// for delta derivation. At that point the semantic value is the
				// shared prefix content, not the historical chain ID.
				lastTurn = cloneCanonicalItems(thread[match.PrefixLength:])
			}
		}
	}

	return NewGenerationRequest(GenerationRequestParams{
		Model:    request.Model(),
		Thread:   thread,
		LastTurn: lastTurn,
	}), nil
}

func (m ContinuationRuntime) CaptureBuffered(
	ctx context.Context,
	namespace ContinuationNamespace,
	request CanonicalRequest,
	output CanonicalOutput,
) error {
	if m.store == nil || namespace.IsZero() {
		return nil
	}
	thread, ok, err := ContinuationConversation(request)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	snapshot, ok, err := BuildContinuitySnapshot(
		thread,
		output,
	)
	if err != nil || !ok {
		return err
	}
	if err := m.store.Store(ctx, namespace, snapshot); err != nil {
		return InternalError("response continuity state could not be stored")
	}
	return nil
}

// WrapStream delays continuity capture until the canonical stream reaches a
// completed output. Storing earlier would risk anchoring future requests to a
// partial assistant turn that no buffered path could ever produce.
func (m ContinuationRuntime) WrapStream(
	ctx context.Context,
	namespace ContinuationNamespace,
	request CanonicalRequest,
	stream CanonicalOutputEventStream,
) (CanonicalOutputEventStream, error) {
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
	return &continuationCapturingCanonicalOutputEventStreamCloser{
		ctx:       ctx,
		store:     m.store,
		namespace: namespace,
		inner:     stream,
		assembler: NewOutputAssembler(request.SemanticKind()),
		thread:    thread,
	}, nil
}

type continuationCapturingCanonicalOutputEventStreamCloser struct {
	ctx       context.Context
	store     ContinuationStore
	namespace ContinuationNamespace
	inner     CanonicalOutputEventStream
	assembler *OutputAssembler
	thread    []CanonicalItem
	stored    bool
}

func (s *continuationCapturingCanonicalOutputEventStreamCloser) Next() (OutputEvent, error) {
	event, err := s.inner.Next()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return OutputEvent{}, io.EOF
		}
		return OutputEvent{}, err
	}
	if applyErr := s.assembler.Apply(event); applyErr != nil {
		return OutputEvent{}, InternalError("canonical output stream could not be assembled for continuation capture")
	}
	if event.Kind == OutputEventCompleted && !s.stored {
		// Continuity capture happens only after full canonical assembly so
		// subsequent requests see the same assistant turn regardless of whether
		// the original caller used buffered or streaming delivery.
		snapshot, ok, buildErr := BuildContinuitySnapshot(
			s.thread,
			s.assembler.Output(),
		)
		if buildErr != nil {
			return OutputEvent{}, buildErr
		}
		if ok {
			if err := s.store.Store(s.ctx, s.namespace, snapshot); err != nil {
				return OutputEvent{}, InternalError("response continuity state could not be stored")
			}
		}
		s.stored = true
	}
	return event, nil
}

func (s *continuationCapturingCanonicalOutputEventStreamCloser) Close() error {
	return s.inner.Close()
}
