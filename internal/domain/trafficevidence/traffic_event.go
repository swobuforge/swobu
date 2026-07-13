package trafficevidence

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// Route identifies the chosen execution destination in traffic-evidence form.
// Model remains optional until the runtime path carries model identity end to end.
type Route struct {
	providerConfigRef string
	model             string
}

func NewRoute(providerConfigRef string, model string) (Route, error) {
	if strings.TrimSpace(providerConfigRef) == "" { // swobu:io-string source=domain
		return Route{}, fmt.Errorf("route provider config ref must not be empty")
	}
	return Route{
		providerConfigRef: providerConfigRef,
		model:             strings.TrimSpace(model), // swobu:io-string source=domain
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
	clientFamily              ClientFamily
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
	wireMutations             []Mutation
	exchangeDiagnostics       []string
	exchangeStageReports      []StageReport
}

type TrafficEventInput struct {
	RequestID                 RequestID
	Endpoint                  string
	ClientProtocol            ClientProtocol
	ClientHandler             ClientHandler
	ClientFamily              ClientFamily
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
	Mutations                 []Mutation
	ExchangeDiagnostics       []string
	StageReports              []StageReport
}

// Mutation is the canonical traffic-evidence row for one wire-patch fact.
// Projectors reuse this exact shape so the evidence and projection contracts do
// not drift apart.
type Mutation struct {
	Stage         string   `json:"leg"`
	PatchID       string   `json:"patch_id"`
	Changed       bool     `json:"changed"`
	ChangedFields []string `json:"changed_fields,omitempty"`
}

// HasChanges reports whether the patch actually mutated the carrier.
func (m Mutation) HasChanges() bool {
	return m.Changed
}

type StageReport struct {
	// Stage is the canonical stage slot identity (for example,
	// provider.wire.out/provider.wire.in) where one wire patch set
	// executed.
	Stage string `json:"stage"`
	// Carrier is the canonical carrier kind mutated or observed by this stage.
	Carrier string `json:"carrier"`
	// Applied lists normalized patch identifiers that ran in this stage.
	Applied []string `json:"applied,omitempty"`
	// Mutated reports whether at least one applied patch changed carrier
	// content for this exchange path.
	Mutated bool `json:"mutated"`
}

func cloneMutations(src []Mutation) []Mutation {
	if len(src) == 0 {
		return nil
	}
	out := make([]Mutation, 0, len(src))
	for _, mutation := range src {
		mutation.ChangedFields = slices.Clone(mutation.ChangedFields)
		out = append(out, mutation)
	}
	return out
}

func cloneStageReports(src []StageReport) []StageReport {
	if len(src) == 0 {
		return nil
	}
	out := make([]StageReport, 0, len(src))
	for _, report := range src {
		report.Applied = slices.Clone(report.Applied)
		out = append(out, report)
	}
	return out
}

func NewTerminalTrafficEvent(input TrafficEventInput) (TrafficEvent, error) {
	return newTrafficEvent(EventKindProviderTerminal, input)
}

func newTrafficEvent(kind EventKind, input TrafficEventInput) (TrafficEvent, error) {
	normalizedInput, err := normalizeTrafficEventInput(kind, input)
	if err != nil {
		return TrafficEvent{}, err
	}
	return TrafficEvent{
		requestID:                 normalizedInput.RequestID,
		eventKind:                 kind,
		endpoint:                  normalizedInput.Endpoint,
		clientProtocol:            normalizedInput.ClientProtocol,
		clientHandler:             normalizedInput.ClientHandler,
		clientFamily:              normalizedInput.ClientFamily,
		normalizedOp:              normalizedInput.NormalizedOp,
		route:                     normalizedInput.Route,
		bridgeID:                  normalizedInput.BridgeID,
		decisionReason:            normalizedInput.DecisionReason,
		adaptationChain:           slices.Clone(normalizedInput.AdaptationChain),
		result:                    normalizedInput.Result,
		statusCode:                normalizedInput.StatusCode,
		timing:                    normalizedInput.Timing,
		attemptCount:              normalizedInput.AttemptCount,
		continuityRecovered:       normalizedInput.ContinuityRecovered,
		continuityRecoveryTrigger: normalizedInput.ContinuityRecoveryTrigger,
		modelResolutionMode:       normalizedInput.ModelResolutionMode,
		modelRequested:            normalizedInput.ModelRequested,
		modelResolved:             normalizedInput.ModelResolved,
		tokenUsage:                normalizedInput.TokenUsage,
		wireMutations:             cloneMutations(normalizedInput.Mutations),
		exchangeDiagnostics:       slices.Clone(normalizedInput.ExchangeDiagnostics),
		exchangeStageReports:      cloneStageReports(normalizedInput.StageReports),
	}, nil
}

func normalizeTrafficEventInput(kind EventKind, input TrafficEventInput) (TrafficEventInput, error) {
	if input.RequestID.IsZero() {
		return TrafficEventInput{}, fmt.Errorf("request id is required")
	}
	if strings.TrimSpace(input.Endpoint) == "" { // swobu:io-string source=domain
		return TrafficEventInput{}, fmt.Errorf("endpoint is required")
	}
	if input.Route.ProviderConfigRef() == "" {
		return TrafficEventInput{}, fmt.Errorf("route is required")
	}
	if input.StatusCode < 0 {
		return TrafficEventInput{}, fmt.Errorf("status code must not be negative")
	}
	switch kind {
	case EventKindProviderInflight:
		if input.Result != ResultClassInProgress {
			return TrafficEventInput{}, fmt.Errorf("in-flight events must use in_progress result class")
		}
		if input.StatusCode != 0 {
			return TrafficEventInput{}, fmt.Errorf("in-flight events must use status code 0")
		}
	case EventKindProviderTerminal:
		if !input.Result.IsTerminal() {
			return TrafficEventInput{}, fmt.Errorf("terminal events must use a terminal result class")
		}
	default:
		return TrafficEventInput{}, fmt.Errorf("unknown event kind %q", kind)
	}
	if input.ClientProtocol == "" {
		input.ClientProtocol = ClientProtocolUnknown
	}
	if input.ClientHandler == "" {
		input.ClientHandler = ClientHandlerUnknown
	}
	if input.ClientFamily == "" {
		input.ClientFamily = ClientFamilyUnknown
	}
	if input.NormalizedOp == "" {
		input.NormalizedOp = NormalizedOpUnknown
	}
	if strings.TrimSpace(input.BridgeID) == "" { // swobu:io-string source=domain
		input.BridgeID = "direct"
	}
	if strings.TrimSpace(input.DecisionReason) == "" { // swobu:io-string source=domain
		input.DecisionReason = "selected_provider_config"
	}
	if input.AttemptCount <= 0 {
		input.AttemptCount = 1
	}
	input.ModelResolutionMode = strings.TrimSpace(input.ModelResolutionMode) // swobu:io-string source=domain
	input.ModelRequested = strings.TrimSpace(input.ModelRequested)           // swobu:io-string source=domain
	input.ModelResolved = strings.TrimSpace(input.ModelResolved)             // swobu:io-string source=domain
	input.Mutations = normalizeTrafficEventMutations(input.Mutations)
	input.ExchangeDiagnostics = normalizeTrafficEventStrings(input.ExchangeDiagnostics)
	stageReports, err := normalizeTrafficEventStageReports(input.StageReports)
	if err != nil {
		return TrafficEventInput{}, err
	}
	input.StageReports = stageReports
	return input, nil
}

func normalizeTrafficEventMutations(src []Mutation) []Mutation {
	normalized := make([]Mutation, 0, len(src))
	for _, m := range src {
		m.Stage = strings.TrimSpace(m.Stage)     // swobu:io-string source=domain
		m.PatchID = strings.TrimSpace(m.PatchID) // swobu:io-string source=domain
		if m.Stage == "" || m.PatchID == "" {
			continue
		}
		m.ChangedFields = normalizeTrafficEventStrings(m.ChangedFields)
		normalized = append(normalized, m)
	}
	return normalized
}

func normalizeTrafficEventStageReports(src []StageReport) ([]StageReport, error) {
	stageReports := make([]StageReport, 0, len(src))
	seenStageCarrier := map[string]struct{}{}
	for _, report := range src {
		report.Stage = strings.ToLower(strings.TrimSpace(report.Stage))     // swobu:io-string source=domain
		report.Carrier = strings.ToLower(strings.TrimSpace(report.Carrier)) // swobu:io-string source=domain
		if report.Stage == "" || report.Carrier == "" {
			return nil, fmt.Errorf("exchange stage report stage/carrier must not be empty")
		}
		report.Applied = normalizeTrafficEventUniqueStrings(report.Applied)
		if report.Mutated && len(report.Applied) == 0 {
			return nil, fmt.Errorf("exchange stage report %q/%q mutated without applied patches", report.Stage, report.Carrier)
		}
		key := report.Stage + "\x00" + report.Carrier
		if _, exists := seenStageCarrier[key]; exists {
			return nil, fmt.Errorf("duplicate exchange stage report for %q/%q", report.Stage, report.Carrier)
		}
		seenStageCarrier[key] = struct{}{}
		stageReports = append(stageReports, report)
	}
	return stageReports, nil
}

func normalizeTrafficEventStrings(src []string) []string {
	normalized := make([]string, 0, len(src))
	for _, value := range src {
		value = strings.TrimSpace(value) // swobu:io-string source=domain
		if value == "" {
			continue
		}
		normalized = append(normalized, value)
	}
	return normalized
}

func normalizeTrafficEventUniqueStrings(src []string) []string {
	normalized := normalizeTrafficEventStrings(src)
	if len(normalized) == 0 {
		return normalized
	}
	seen := map[string]struct{}{}
	unique := make([]string, 0, len(normalized))
	for _, value := range normalized {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	sort.Strings(unique)
	return unique
}

func (e TrafficEvent) RequestID() RequestID              { return e.requestID }
func (e TrafficEvent) EventKind() EventKind              { return e.eventKind }
func (e TrafficEvent) Endpoint() string                  { return e.endpoint }
func (e TrafficEvent) ClientProtocol() ClientProtocol    { return e.clientProtocol }
func (e TrafficEvent) ClientHandler() ClientHandler      { return e.clientHandler }
func (e TrafficEvent) ClientFamily() ClientFamily        { return e.clientFamily }
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
func (e TrafficEvent) Mutations() []Mutation {
	return cloneMutations(e.wireMutations)
}
func (e TrafficEvent) ExchangeDiagnostics() []string {
	return slices.Clone(e.exchangeDiagnostics)
}
func (e TrafficEvent) StageReports() []StageReport {
	return cloneStageReports(e.exchangeStageReports)
}
