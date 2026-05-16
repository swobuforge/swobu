package runtimeevidence

import (
	"fmt"
	"slices"
	"strings"
)

type EventKind string

const (
	EventKindUpstreamInflight EventKind = "upstream_inflight"
	EventKindUpstreamTerminal EventKind = "upstream_terminal"
)

type ClientProtocol string

const ClientProtocolUnknown ClientProtocol = "unknown"

type ClientHandler string

const ClientHandlerUnknown ClientHandler = "unknown"

type IngressFamily string

const IngressFamilyUnknown IngressFamily = "unknown"

type NormalizedOp string

const NormalizedOpUnknown NormalizedOp = "unknown"

// Route identifies the chosen execution destination in evidence-friendly form.
// Model remains optional until the runtime path carries model identity end to end.
type Route struct {
	providerConfigRef string
	model             string
}

func NewRoute(providerConfigRef string, model string) (Route, error) {
	if strings.TrimSpace(providerConfigRef) == "" { // trimlowerlint:allow domain canonicalization
		return Route{}, fmt.Errorf("route provider config ref must not be empty")
	}
	return Route{
		providerConfigRef: providerConfigRef,
		model:             strings.TrimSpace(model), // trimlowerlint:allow domain canonicalization
	}, nil
}

func (r Route) ProviderConfigRef() string { return r.providerConfigRef }
func (r Route) Model() string             { return r.model }

func (r Route) String() string {
	if r.model == "" {
		return r.providerConfigRef
	}
	return r.providerConfigRef + ":" + r.model
}

// Timing records observed latency facts without guessing missing values.
type Timing struct {
	ttfbMS    int
	durMS     int
	hasTTFBMS bool
	hasDurMS  bool
}

func NewUnknownTiming() Timing {
	return Timing{}
}

func NewTiming(ttfbMS int, durMS int) (Timing, error) {
	return NewTimingWithOptional(&ttfbMS, &durMS)
}

func NewTimingWithOptional(ttfbMS *int, durMS *int) (Timing, error) {
	timing := Timing{}
	if ttfbMS != nil {
		if *ttfbMS < 0 {
			return Timing{}, fmt.Errorf("ttfb must not be negative")
		}
		timing.ttfbMS = *ttfbMS
		timing.hasTTFBMS = true
	}
	if durMS != nil {
		if *durMS < 0 {
			return Timing{}, fmt.Errorf("duration must not be negative")
		}
		timing.durMS = *durMS
		timing.hasDurMS = true
	}
	if timing.hasTTFBMS && timing.hasDurMS && timing.durMS < timing.ttfbMS {
		return Timing{}, fmt.Errorf("duration must not be less than ttfb")
	}
	return timing, nil
}

func (t Timing) TTFBMillis() (int, bool) {
	return t.ttfbMS, t.hasTTFBMS
}

func (t Timing) DurationMillis() (int, bool) {
	return t.durMS, t.hasDurMS
}

type TrafficEvent struct {
	requestID                 RequestID
	eventKind                 EventKind
	endpoint                  string
	clientProtocol            ClientProtocol
	clientHandler             ClientHandler
	ingressFamily             IngressFamily
	normalizedOp              NormalizedOp
	route                     Route
	bridgeID                  string
	decisionReason            string
	adaptationChain           []string
	result                    ResultClass
	statusCode                int
	timing                    Timing
	attemptCount              int
	continuityRecovered       bool
	continuityRecoveryTrigger string
	modelResolutionMode       string
	modelRequested            string
	modelResolved             string
	tokenUsage                TokenUsage
}

type TrafficEventInput struct {
	RequestID                 RequestID
	Endpoint                  string
	ClientProtocol            ClientProtocol
	ClientHandler             ClientHandler
	IngressFamily             IngressFamily
	NormalizedOp              NormalizedOp
	Route                     Route
	BridgeID                  string
	DecisionReason            string
	AdaptationChain           []string
	Result                    ResultClass
	StatusCode                int
	Timing                    Timing
	AttemptCount              int
	ContinuityRecovered       bool
	ContinuityRecoveryTrigger string
	ModelResolutionMode       string
	ModelRequested            string
	ModelResolved             string
	TokenUsage                TokenUsage
}

// NewInflightTrafficEvent creates the first immutable evidence fact for a
// request lifecycle. Unknown protocol-facing fields remain explicit `unknown`
// values until later seams can enrich them without changing event shape.
func NewInflightTrafficEvent(input TrafficEventInput) (TrafficEvent, error) {
	input.Result = ResultClassInProgress
	input.StatusCode = 0
	return newTrafficEvent(EventKindUpstreamInflight, input)
}

func NewTerminalTrafficEvent(input TrafficEventInput) (TrafficEvent, error) {
	return newTrafficEvent(EventKindUpstreamTerminal, input)
}

// evidence schema's validation and normalization rules.
func newTrafficEvent(kind EventKind, input TrafficEventInput) (TrafficEvent, error) {
	if input.RequestID.IsZero() {
		return TrafficEvent{}, fmt.Errorf("request id is required")
	}
	if strings.TrimSpace(input.Endpoint) == "" { // trimlowerlint:allow domain canonicalization
		return TrafficEvent{}, fmt.Errorf("endpoint is required")
	}
	if input.Route.ProviderConfigRef() == "" {
		return TrafficEvent{}, fmt.Errorf("route is required")
	}
	if input.ClientProtocol == "" {
		input.ClientProtocol = ClientProtocolUnknown
	}
	if input.ClientHandler == "" {
		input.ClientHandler = ClientHandlerUnknown
	}
	if input.IngressFamily == "" {
		input.IngressFamily = IngressFamilyUnknown
	}
	if input.NormalizedOp == "" {
		input.NormalizedOp = NormalizedOpUnknown
	}
	if strings.TrimSpace(input.BridgeID) == "" { // trimlowerlint:allow domain canonicalization
		input.BridgeID = "direct"
	}
	if strings.TrimSpace(input.DecisionReason) == "" { // trimlowerlint:allow domain canonicalization
		input.DecisionReason = "selected_provider_config"
	}
	if input.StatusCode < 0 {
		return TrafficEvent{}, fmt.Errorf("status code must not be negative")
	}
	if input.AttemptCount <= 0 {
		input.AttemptCount = 1
	}
	input.ModelResolutionMode = strings.TrimSpace(input.ModelResolutionMode) // trimlowerlint:allow domain canonicalization
	input.ModelRequested = strings.TrimSpace(input.ModelRequested)           // trimlowerlint:allow domain canonicalization
	input.ModelResolved = strings.TrimSpace(input.ModelResolved)             // trimlowerlint:allow domain canonicalization
	switch kind {
	case EventKindUpstreamInflight:
		if input.Result != ResultClassInProgress {
			return TrafficEvent{}, fmt.Errorf("in-flight events must use in_progress result class")
		}
		if input.StatusCode != 0 {
			return TrafficEvent{}, fmt.Errorf("in-flight events must use status code 0")
		}
	case EventKindUpstreamTerminal:
		if !input.Result.IsTerminal() {
			return TrafficEvent{}, fmt.Errorf("terminal events must use a terminal result class")
		}
	default:
		return TrafficEvent{}, fmt.Errorf("unknown event kind %q", kind)
	}
	return TrafficEvent{
		requestID:                 input.RequestID,
		eventKind:                 kind,
		endpoint:                  input.Endpoint,
		clientProtocol:            input.ClientProtocol,
		clientHandler:             input.ClientHandler,
		ingressFamily:             input.IngressFamily,
		normalizedOp:              input.NormalizedOp,
		route:                     input.Route,
		bridgeID:                  input.BridgeID,
		decisionReason:            input.DecisionReason,
		adaptationChain:           slices.Clone(input.AdaptationChain),
		result:                    input.Result,
		statusCode:                input.StatusCode,
		timing:                    input.Timing,
		attemptCount:              input.AttemptCount,
		continuityRecovered:       input.ContinuityRecovered,
		continuityRecoveryTrigger: input.ContinuityRecoveryTrigger,
		modelResolutionMode:       input.ModelResolutionMode,
		modelRequested:            input.ModelRequested,
		modelResolved:             input.ModelResolved,
		tokenUsage:                input.TokenUsage,
	}, nil
}

func (e TrafficEvent) RequestID() RequestID              { return e.requestID }
func (e TrafficEvent) EventKind() EventKind              { return e.eventKind }
func (e TrafficEvent) Endpoint() string                  { return e.endpoint }
func (e TrafficEvent) ClientProtocol() ClientProtocol    { return e.clientProtocol }
func (e TrafficEvent) ClientHandler() ClientHandler      { return e.clientHandler }
func (e TrafficEvent) IngressFamily() IngressFamily      { return e.ingressFamily }
func (e TrafficEvent) NormalizedOp() NormalizedOp        { return e.normalizedOp }
func (e TrafficEvent) Route() Route                      { return e.route }
func (e TrafficEvent) BridgeID() string                  { return e.bridgeID }
func (e TrafficEvent) DecisionReason() string            { return e.decisionReason }
func (e TrafficEvent) AdaptationChain() []string         { return slices.Clone(e.adaptationChain) }
func (e TrafficEvent) Result() ResultClass               { return e.result }
func (e TrafficEvent) StatusCode() int                   { return e.statusCode }
func (e TrafficEvent) Timing() Timing                    { return e.timing }
func (e TrafficEvent) AttemptCount() int                 { return e.attemptCount }
func (e TrafficEvent) ContinuityRecovered() bool         { return e.continuityRecovered }
func (e TrafficEvent) ContinuityRecoveryTrigger() string { return e.continuityRecoveryTrigger }
func (e TrafficEvent) ModelResolutionMode() string       { return e.modelResolutionMode }
func (e TrafficEvent) ModelRequested() string            { return e.modelRequested }
func (e TrafficEvent) ModelResolved() string             { return e.modelResolved }
func (e TrafficEvent) TokenUsage() TokenUsage            { return e.tokenUsage }
