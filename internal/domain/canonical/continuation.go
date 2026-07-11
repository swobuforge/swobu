package canonical

import (
	"context"
	"strings"
	"time"

	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

// ContinuationNamespace is the internal compatibility-owned partition key used
// to scope replay chains without exposing a client-visible session noun.
type ContinuationNamespace string

// NewContinuationNamespace normalizes one request-path namespace for internal
// continuation storage and replay lookups.
func NewContinuationNamespace(raw string) ContinuationNamespace {
	return ContinuationNamespace(strings.TrimSpace(raw)) // swobu:io-string source=domain
}

func (s ContinuationNamespace) IsZero() bool {
	return strings.TrimSpace(string(s)) == "" // swobu:io-string source=domain
}

func (s ContinuationNamespace) String() string {
	return string(s)
}

// ContinuationID is the Swobu-owned logical handle for one continuation chain.
type ContinuationID string

// NewContinuationID normalizes one continuation handle into canonical form.
func NewContinuationID(raw string) ContinuationID {
	return ContinuationID(strings.TrimSpace(raw)) // swobu:io-string source=domain
}

func (id ContinuationID) IsZero() bool {
	return strings.TrimSpace(string(id)) == "" // swobu:io-string source=domain
}

func (id ContinuationID) String() string {
	return string(id)
}

// Clone returns a stable copy of the logical continuation handle.
func (id ContinuationID) Clone() ContinuationID {
	return ContinuationID(string(id))
}

// TurnRef captures request-scoped semantic continuation intent.
type TurnRef struct {
	Previous *ContinuationID
}

// NewTurnRef converts a wire-level parent selector into canonical turn intent.
func NewTurnRef(previous string) TurnRef {
	id := NewContinuationID(previous)
	if id.IsZero() {
		return TurnRef{}
	}
	return TurnRef{Previous: &id}
}

func (r TurnRef) IsZero() bool {
	return r.Previous == nil || r.Previous.IsZero()
}

func (r TurnRef) PreviousID() (ContinuationID, bool) {
	if r.Previous == nil || r.Previous.IsZero() {
		return ContinuationID(""), false
	}
	return r.Previous.Clone(), true
}

func (r TurnRef) Clone() TurnRef {
	if r.Previous == nil {
		return TurnRef{}
	}
	id := r.Previous.Clone()
	return TurnRef{Previous: &id}
}

// ContinuationStatus records whether one continuation record is still open or
// has reached a terminal outcome.
type ContinuationStatus string

const (
	ContinuationStatusInProgress ContinuationStatus = "in_progress"
	ContinuationStatusCompleted  ContinuationStatus = "completed"
	ContinuationStatusFailed     ContinuationStatus = "failed"
)

// ContinuationRecord is the semantic chain unit persisted by ContinuationStore.
// It carries the request delta and response snapshot separately so materialized
// replay can be reconstructed without conflating semantic history with opaque
// provider-native state.
type ContinuationRecord struct {
	ID           ContinuationID
	Parent       *ContinuationID
	RouteID      string
	Client       ClientFamily
	Provider     protocolkind.ProtocolKind
	ModelID      string
	RequestDelta CanonicalRequest
	Response     CanonicalOutputData
	Status       ContinuationStatus
	CreatedAt    time.Time
	ExpiresAt    *time.Time
}

// Clone returns a deep copy of the semantic record.
func (r ContinuationRecord) Clone() ContinuationRecord {
	var parent *ContinuationID
	if r.Parent != nil {
		id := r.Parent.Clone()
		parent = &id
	}
	var expiresAt *time.Time
	if r.ExpiresAt != nil {
		cloned := *r.ExpiresAt
		expiresAt = &cloned
	}
	return ContinuationRecord{
		ID:           r.ID.Clone(),
		Parent:       parent,
		RouteID:      r.RouteID,
		Client:       r.Client,
		Provider:     r.Provider,
		ModelID:      r.ModelID,
		RequestDelta: r.RequestDelta.Clone(),
		Response:     NewOutputWithUsage(r.Response.semanticKind, r.Response.resultID, r.Response.model, r.Response.items, r.Response.finishReason, r.Response.usage),
		Status:       r.Status,
		CreatedAt:    r.CreatedAt,
		ExpiresAt:    expiresAt,
	}
}

// ContinuationStore persists Swobu-owned semantic continuation records.
type ContinuationStore interface {
	Put(ctx context.Context, rec ContinuationRecord) error
	Get(ctx context.Context, id ContinuationID) (ContinuationRecord, bool, error)
	Chain(ctx context.Context, id ContinuationID) ([]ContinuationRecord, error)
}

// ContinuationRuntime prepares request-path continuation semantics and captures
// completed continuation records without leaking provider-native replay bytes
// into the semantic request model.
type ContinuationRuntime struct {
	store ContinuationStore
}

// NewContinuationRuntime creates a request-path continuation helper around one
// semantic continuation store.
func NewContinuationRuntime(store ContinuationStore) ContinuationRuntime {
	return ContinuationRuntime{store: store}
}

// PrepareRequest rewrites continuation-aware requests so the selected target
// receives either a native continuation-safe delta or a fully materialized
// semantic thread, depending on the target protocol.
func (m ContinuationRuntime) PrepareRequest(ctx context.Context, namespace ContinuationNamespace, targetProtocol protocolkind.ProtocolKind, request CanonicalRequest) (CanonicalRequest, error) {
	if request.Turn().IsZero() {
		return CloneCanonicalRequest(request), nil
	}
	if m.store == nil {
		if targetProtocol == protocolkind.Responses {
			return CloneCanonicalRequest(request), nil
		}
		return CanonicalRequest{}, BadRequest("continuation selector could not be rehydrated")
	}
	previousID, ok := request.Turn().PreviousID()
	if !ok {
		return CloneCanonicalRequest(request), nil
	}
	chain, err := m.store.Chain(ctx, previousID)
	if err != nil {
		return CanonicalRequest{}, InternalError("response continuity state could not be loaded")
	}
	if len(chain) == 0 {
		return CanonicalRequest{}, BadRequest("continuation selector could not be rehydrated")
	}
	anchor := materializeContinuationThread(chain)
	currentThread := request.Items()
	prefixLen := longestCommonPrefixLength(anchor, currentThread)

	thread := cloneCanonicalItems(currentThread)
	keepNativeContinuation := targetProtocol == protocolkind.Responses
	switch {
	case len(currentThread) == 0:
		thread = cloneCanonicalItems(anchor)
	case prefixLen == len(anchor):
		thread = cloneCanonicalItems(currentThread)
	case prefixLen == 0:
		thread = append(cloneCanonicalItems(anchor), cloneCanonicalItems(currentThread)...)
	default:
		thread = cloneCanonicalItems(currentThread)
		keepNativeContinuation = false
	}

	preparedTurn := TurnRef{}
	if keepNativeContinuation {
		preparedTurn = request.Turn().Clone()
	}

	// When the target cannot consume native continuation safely, the semantic
	// thread is materialized and the native selector is cleared to avoid
	// duplicate history injection.
	return NewCanonicalRequest(RequestParams{
		Model:       request.Model(),
		Items:       thread,
		Tools:       request.Tools(),
		Turn:        preparedTurn,
		ToolPolicy:  request.ToolPolicy(),
		CacheIntent: request.CacheIntent(),
	}), nil
}

// WrapResponseEnvelope captures the completed semantic continuation record once
// the closed response envelope is available.
func (m ContinuationRuntime) WrapResponseEnvelope(
	ctx context.Context,
	namespace ContinuationNamespace,
	request CanonicalRequest,
	stream EventReader,
) (EventReader, error) {
	if m.store == nil || namespace.IsZero() || stream == nil {
		return stream, nil
	}
	return &continuationCapturingEnvelopeEventReader{
		ctx:       ctx,
		store:     m.store,
		namespace: namespace,
		inner:     stream,
		request:   request.Clone(),
		index:     NewEnvelopeIndex(),
	}, nil
}

type continuationCapturingEnvelopeEventReader struct {
	ctx       context.Context
	store     ContinuationStore
	namespace ContinuationNamespace
	inner     EventReader
	request   CanonicalRequest
	index     *EnvelopeIndex
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
			record, ok, buildErr := buildContinuationRecord(r.namespace, r.request, *output, time.Now().UTC())
			if buildErr != nil {
				return Event{}, buildErr
			}
			if ok {
				if storeErr := r.store.Put(r.ctx, record); storeErr != nil {
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

func buildContinuationRecord(namespace ContinuationNamespace, request CanonicalRequest, output CanonicalOutput, now time.Time) (ContinuationRecord, bool, error) {
	if output == nil || output.ResultID() == "" {
		return ContinuationRecord{}, false, nil
	}
	response, ok := output.CloneOutput().(CanonicalOutputData)
	if !ok {
		return ContinuationRecord{}, false, InternalError("canonical output snapshot could not be cloned for continuation capture")
	}
	requestDelta := currentTurnDelta(request.Items())
	delta := NewCanonicalRequest(RequestParams{
		Model:       request.Model(),
		Items:       requestDelta,
		Tools:       request.Tools(),
		Turn:        TurnRef{},
		ToolPolicy:  request.ToolPolicy(),
		CacheIntent: request.CacheIntent(),
	})
	record := ContinuationRecord{
		ID:           NewContinuationID(output.ResultID()),
		RouteID:      namespace.String(),
		ModelID:      request.Model(),
		RequestDelta: delta,
		Response:     response,
		Status:       ContinuationStatusCompleted,
		CreatedAt:    now,
	}
	if previousID, ok := request.Turn().PreviousID(); ok {
		parent := previousID.Clone()
		record.Parent = &parent
	}
	return record, true, nil
}

func materializeContinuationThread(chain []ContinuationRecord) []CanonicalItem {
	if len(chain) == 0 {
		return nil
	}
	out := make([]CanonicalItem, 0, len(chain)*2)
	for _, record := range chain {
		out = append(out, record.RequestDelta.Items()...)
		out = append(out, record.Response.Items()...)
	}
	return out
}

// CurrentTurnDelta returns the suffix that begins at the latest user-authored
// item. It is the materialized last-turn view used when a request needs to be
// replayed or captured without duplicating prior context.
func CurrentTurnDelta(items []CanonicalItem) []CanonicalItem {
	return currentTurnDelta(items)
}

func currentTurnDelta(items []CanonicalItem) []CanonicalItem {
	if len(items) == 0 {
		return nil
	}
	lastUser := -1
	for i := len(items) - 1; i >= 0; i-- {
		if items[i].Author == ItemAuthorUser {
			lastUser = i
			break
		}
	}
	if lastUser < 0 {
		return cloneCanonicalItems(items)
	}
	return append([]CanonicalItem(nil), items[lastUser:]...)
}

func longestCommonPrefixLength(left []CanonicalItem, right []CanonicalItem) int {
	limit := len(left)
	if len(right) < limit {
		limit = len(right)
	}
	for i := 0; i < limit; i++ {
		if !canonicalItemsSemanticEqual(left[i], right[i]) {
			return i
		}
	}
	return limit
}

func canonicalItemsSemanticEqual(left CanonicalItem, right CanonicalItem) bool {
	return left.Author == right.Author &&
		left.Kind == right.Kind &&
		left.Text == right.Text &&
		left.ToolUseID == right.ToolUseID &&
		left.Name == right.Name &&
		left.Input.RawObject() == right.Input.RawObject()
}
