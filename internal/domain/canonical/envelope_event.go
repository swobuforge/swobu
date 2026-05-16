package canonical

import "time"

type EnvelopeID string

type EventKind string

const (
	EventEnvelopeStart EventKind = "envelope.start"
	EventEnvelopeEnd   EventKind = "envelope.end"
	EventTextDelta     EventKind = "text.delta"
	EventArgsDelta     EventKind = "args.delta"
	EventUsage         EventKind = "usage"
	EventFinish        EventKind = "finish"
	EventError         EventKind = "error"
	EventMetadata      EventKind = "metadata"
)

type EnvelopeKind string

const (
	EnvExchange   EnvelopeKind = "exchange"
	EnvRequest    EnvelopeKind = "request"
	EnvResponse   EnvelopeKind = "response"
	EnvMessage    EnvelopeKind = "message"
	EnvToolCall   EnvelopeKind = "tool_call"
	EnvToolResult EnvelopeKind = "tool_result"
	EnvReasoning  EnvelopeKind = "reasoning"
)

type EventSource string

const (
	EventSourceClient EventSource = "client"
	EventSourceVendor EventSource = "vendor"
	EventSourceCore   EventSource = "core"
)

type EventMeta struct {
	// Source marks the emitting edge (client/vendor/core) for provenance.
	Source    EventSource
	Vendor    string
	Model     string
	Protocol  string
	Transport string

	// Synthetic marks events generated from a buffered snapshot rather than
	// directly decoded from a native stream.
	Synthetic bool
	// Buffered marks events that crossed a projection/materialization boundary.
	Buffered bool
	// Degraded marks behavior downgrades (for example stream-shaped batch).
	Degraded bool

	// Native identifiers are references only; canonical envelope IDs stay
	// primary and stable throughout the exchange.
	NativeID    string
	NativeIndex *int
	Raw         any
}

// Event is the canonical envelope event used as internal wire truth.
type Event struct {
	ExchangeID string
	Seq        int64
	Time       time.Time

	Kind     EventKind
	EnvID    EnvelopeID
	ParentID EnvelopeID

	Payload any
	Meta    EventMeta
}

type EnvelopeStatus string

const (
	EnvelopeStatusCompleted EnvelopeStatus = "completed"
	EnvelopeStatusError     EnvelopeStatus = "error"
)

type EnvelopeStartPayload struct {
	Kind      EnvelopeKind
	Role      ItemAuthor
	Name      string
	ToolUseID string
}

type EnvelopeEndPayload struct {
	Kind   EnvelopeKind
	Status EnvelopeStatus
}

type TextDeltaPayload struct {
	Text string
}

type ArgsDeltaPayload struct {
	Args string
}

type UsagePayload struct {
	Usage TokenUsage
}

type FinishPayload struct {
	Reason string
}

type ErrorPayload struct {
	Code      string
	Message   string
	Retryable bool
}

type MetadataPayload struct {
	Values map[string]string
}

// AliasKey indexes native protocol identity facts to a canonical envelope ID.
// It is intentionally edge-facing and never replaces canonical IDs.
type AliasKey struct {
	Protocol string
	Kind     string
	NativeID string
	Index    int
}

type AliasTable struct {
	nativeToCanonical map[AliasKey]EnvelopeID
}

func NewAliasTable() AliasTable {
	return AliasTable{nativeToCanonical: map[AliasKey]EnvelopeID{}}
}

func (a *AliasTable) Get(key AliasKey) (EnvelopeID, bool) {
	if a == nil || a.nativeToCanonical == nil {
		return "", false
	}
	id, ok := a.nativeToCanonical[key]
	return id, ok
}

func (a *AliasTable) Put(key AliasKey, id EnvelopeID) {
	if a == nil {
		return
	}
	if a.nativeToCanonical == nil {
		a.nativeToCanonical = map[AliasKey]EnvelopeID{}
	}
	a.nativeToCanonical[key] = id
}
