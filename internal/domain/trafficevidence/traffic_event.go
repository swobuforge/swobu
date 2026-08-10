package trafficevidence

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/profile"
)

// Route identifies the chosen execution destination in traffic-evidence form.
// Model remains optional until the runtime path carries model identity end to end.
type Route struct {
	targetID string
	model    string
}

func NewRoute(targetID string, model string) (Route, error) {
	if strings.TrimSpace(targetID) == "" { // swobu:io-string source=domain
		return Route{}, fmt.Errorf("route target ID must not be empty")
	}
	return Route{
		targetID: targetID,
		model:    strings.TrimSpace(model), // swobu:io-string source=domain
	}, nil
}

func (r Route) TargetID() string { return r.targetID }
func (r Route) Model() string    { return r.model }

func (r Route) String() string {
	if r.model == "" {
		return r.targetID
	}
	return r.targetID + ":" + r.model
}

// Timing records request-lifecycle marks and derives latency summaries when
// projected.
type Timing struct {
	startedAt    time.Time
	firstByteAt  time.Time
	endedAt      time.Time
	hasStarted   bool
	hasFirstByte bool
	hasEnded     bool
}

func NewUnknownTiming() Timing {
	return Timing{}
}

func NewTimingStartedAt(startedAt time.Time) Timing {
	timing := Timing{}
	timing.MarkStarted(startedAt)
	return timing
}

func NewTimingWithOptional(ttfbMS *int, durMS *int) (Timing, error) {
	timing := Timing{}
	base := time.Unix(0, 0)
	if ttfbMS != nil {
		if *ttfbMS < 0 {
			return Timing{}, fmt.Errorf("ttfb must not be negative")
		}
		timing.startedAt = base
		timing.firstByteAt = base.Add(time.Duration(*ttfbMS) * time.Millisecond)
		timing.hasStarted = true
		timing.hasFirstByte = true
	}
	if durMS != nil {
		if *durMS < 0 {
			return Timing{}, fmt.Errorf("duration must not be negative")
		}
		if !timing.hasStarted {
			timing.startedAt = base
			timing.hasStarted = true
		}
		timing.endedAt = base.Add(time.Duration(*durMS) * time.Millisecond)
		timing.hasEnded = true
	}
	if timing.hasFirstByte && timing.hasEnded && timing.endedAt.Before(timing.firstByteAt) {
		return Timing{}, fmt.Errorf("duration must not be less than ttfb")
	}
	return timing, nil
}

func (t *Timing) MarkStarted(startedAt time.Time) {
	if t == nil || startedAt.IsZero() {
		return
	}
	if t.hasStarted {
		return
	}
	t.startedAt = startedAt
	t.hasStarted = true
}

func (t *Timing) MarkFirstByte(firstByteAt time.Time) {
	if t == nil || firstByteAt.IsZero() {
		return
	}
	if !t.hasStarted || t.hasFirstByte {
		return
	}
	t.firstByteAt = firstByteAt
	t.hasFirstByte = true
}

func (t *Timing) MarkEnded(endedAt time.Time) {
	if t == nil || endedAt.IsZero() {
		return
	}
	t.endedAt = endedAt
	t.hasEnded = true
}

func (t Timing) StartedAt() (time.Time, bool) {
	return t.startedAt, t.hasStarted
}

func (t Timing) FirstByteAt() (time.Time, bool) {
	return t.firstByteAt, t.hasFirstByte
}

func (t Timing) EndedAt() (time.Time, bool) {
	return t.endedAt, t.hasEnded
}

func (t Timing) TTFBMillis() (int, bool) {
	if !t.hasStarted || !t.hasFirstByte {
		return 0, false
	}
	if t.firstByteAt.Before(t.startedAt) {
		return 0, false
	}
	return int(t.firstByteAt.Sub(t.startedAt) / time.Millisecond), true
}

func (t Timing) DurationMillis() (int, bool) {
	if !t.hasStarted || !t.hasEnded {
		return 0, false
	}
	if t.endedAt.Before(t.startedAt) {
		return 0, false
	}
	return int(t.endedAt.Sub(t.startedAt) / time.Millisecond), true
}

type TrafficEvent struct {
	requestID                 RequestID
	eventKind                 EventKind
	workspace                 string
	clientProtocol            ClientProtocol
	requestPath               canonical.NormalizedPath
	clientHandler             ClientHandler
	clientFamily              ClientFamily
	normalizedOp              NormalizedOp
	route                     Route
	bridgeID                  string
	decisionReason            string
	adaptationChain           []string
	result                    ResultClass
	statusCode                int
	deliveryKind              delivery.ResultKind
	canonicalErrorCode        canonical.ErrorCode
	timing                    Timing
	attemptCount              int
	fallbackRecovered         bool
	continuityRecovered       bool
	continuityRecoveryTrigger string
	modelResolutionMode       string
	modelRequested            string
	modelResolved             string
	workspaceRouteModelID     string
	providerSpec              profile.ProviderID
	providerModel             string
	tokenUsage                TokenUsage
	wireMutations             []Mutation
	exchangeDiagnostics       []string
	exchangeStageReports      []StageReport
}

type TrafficEventInput struct {
	RequestID                 RequestID
	Workspace                 string
	ClientProtocol            ClientProtocol
	RequestPath               canonical.NormalizedPath
	ClientHandler             ClientHandler
	ClientFamily              ClientFamily
	NormalizedOp              NormalizedOp
	Route                     Route
	BridgeID                  string
	DecisionReason            string
	AdaptationChain           []string
	Timing                    Timing
	ContinuityRecovered       bool
	ContinuityRecoveryTrigger string
	ModelResolutionMode       string
	ModelRequested            string
	ModelResolved             string
	WorkspaceRouteModelID     string
	ProviderSpec              profile.ProviderID
	ProviderModel             string
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

// TerminalOutcome is the terminal-only fact band: the concrete delivery result
// and its routing/recovery context. It is composed into an event only by
// NewTerminalTrafficEvent, so a non-terminal event cannot represent a terminal
// delivery result, a recovered fallback, or a terminal error code — the invalid
// aggregate is not representable, not merely rejected at runtime.
type TerminalOutcome struct {
	Result             ResultClass
	StatusCode         int
	DeliveryKind       delivery.ResultKind
	CanonicalErrorCode canonical.ErrorCode
	AttemptCount       int
	FallbackRecovered  bool
}

// NewProviderInflightTrafficEvent records that one concrete provider call has
// begun. It carries selected route/target evidence but no terminal delivery,
// error, recovery, or duration claim.
func NewProviderInflightTrafficEvent(base TrafficEventInput, attemptCount int) (TrafficEvent, error) {
	normalized, err := normalizeTrafficEventInput(base)
	if err != nil {
		return TrafficEvent{}, err
	}
	if attemptCount <= 0 {
		return TrafficEvent{}, fmt.Errorf("in-flight attempt count must be positive")
	}
	if !canonical.ValidNormalizedPath(normalized.RequestPath) {
		return TrafficEvent{}, fmt.Errorf("in-flight traffic event request path %q is not canonical", normalized.RequestPath)
	}
	if _, ok := profile.ParseProviderID(string(normalized.ProviderSpec)); !ok {
		return TrafficEvent{}, fmt.Errorf("in-flight traffic event provider %q is not a catalog id", normalized.ProviderSpec)
	}
	event := newTrafficEventBase(normalized)
	event.eventKind = EventKindProviderInflight
	event.result = ResultClassInProgress
	event.attemptCount = attemptCount
	return event, nil
}

// validStatus reports whether code is a transport-valid HTTP status for a terminal
// event: 0 (no response received) or a real status in 100-599. It matches the V1
// schema's status_code constraint, so every constructed terminal event is
// transport-valid, not merely nonnegative.
func validStatus(code int) bool {
	return code == 0 || (code >= 100 && code <= 599)
}

// NewTerminalTrafficEvent composes a base of kind-agnostic facts with the
// terminal-only outcome band. Terminal facts live on TerminalOutcome, not the
// shared input; the owned domain types are validated once here, the single
// chokepoint that protects every traffic-evidence consumer (telemetry included).
func NewTerminalTrafficEvent(base TrafficEventInput, outcome TerminalOutcome) (TrafficEvent, error) {
	normalized, err := normalizeTrafficEventInput(base)
	if err != nil {
		return TrafficEvent{}, err
	}
	if !outcome.Result.IsTerminal() {
		return TrafficEvent{}, fmt.Errorf("terminal events must use a terminal result class")
	}
	if !validStatus(outcome.StatusCode) {
		return TrafficEvent{}, fmt.Errorf("terminal traffic event status code %d is not a valid HTTP status (0 or 100-599)", outcome.StatusCode)
	}
	if outcome.AttemptCount < 0 {
		return TrafficEvent{}, fmt.Errorf("attempt count cannot be negative")
	}
	if !canonical.ValidNormalizedPath(normalized.RequestPath) {
		return TrafficEvent{}, fmt.Errorf("terminal traffic event request path %q is not canonical", normalized.RequestPath)
	}
	if _, ok := profile.ParseProviderID(string(normalized.ProviderSpec)); !ok {
		return TrafficEvent{}, fmt.Errorf("terminal traffic event provider %q is not a catalog id", normalized.ProviderSpec)
	}
	if !delivery.ValidResultKind(outcome.DeliveryKind) {
		return TrafficEvent{}, fmt.Errorf("terminal traffic event delivery kind %q is not recognized", outcome.DeliveryKind)
	}
	if outcome.CanonicalErrorCode != "" && !canonical.ValidErrorCode(outcome.CanonicalErrorCode) {
		return TrafficEvent{}, fmt.Errorf("terminal traffic event canonical error code %q is not recognized", outcome.CanonicalErrorCode)
	}
	event := newTrafficEventBase(normalized)
	event.eventKind = EventKindProviderTerminal
	event.result = outcome.Result
	event.statusCode = outcome.StatusCode
	event.deliveryKind = outcome.DeliveryKind
	event.canonicalErrorCode = outcome.CanonicalErrorCode
	event.attemptCount = outcome.AttemptCount
	event.fallbackRecovered = outcome.FallbackRecovered
	event.tokenUsage = normalized.TokenUsage
	event.wireMutations = cloneMutations(normalized.Mutations)
	event.exchangeDiagnostics = slices.Clone(normalized.ExchangeDiagnostics)
	event.exchangeStageReports = cloneStageReports(normalized.StageReports)
	return event, nil
}

func newTrafficEventBase(normalized TrafficEventInput) TrafficEvent {
	return TrafficEvent{
		requestID:                 normalized.RequestID,
		workspace:                 normalized.Workspace,
		clientProtocol:            normalized.ClientProtocol,
		requestPath:               normalized.RequestPath,
		clientHandler:             normalized.ClientHandler,
		clientFamily:              normalized.ClientFamily,
		normalizedOp:              normalized.NormalizedOp,
		route:                     normalized.Route,
		bridgeID:                  normalized.BridgeID,
		decisionReason:            normalized.DecisionReason,
		adaptationChain:           slices.Clone(normalized.AdaptationChain),
		timing:                    normalized.Timing,
		continuityRecovered:       normalized.ContinuityRecovered,
		continuityRecoveryTrigger: normalized.ContinuityRecoveryTrigger,
		modelResolutionMode:       normalized.ModelResolutionMode,
		modelRequested:            normalized.ModelRequested,
		modelResolved:             normalized.ModelResolved,
		workspaceRouteModelID:     normalized.WorkspaceRouteModelID,
		providerSpec:              normalized.ProviderSpec,
		providerModel:             strings.TrimSpace(normalized.ProviderModel), // swobu:io-string source=boundary
	}
}

func normalizeTrafficEventInput(input TrafficEventInput) (TrafficEventInput, error) {
	if input.RequestID.IsZero() {
		return TrafficEventInput{}, fmt.Errorf("request id is required")
	}
	if strings.TrimSpace(input.Workspace) == "" { // swobu:io-string source=domain
		return TrafficEventInput{}, fmt.Errorf("endpoint is required")
	}
	if input.Route.TargetID() == "" {
		return TrafficEventInput{}, fmt.Errorf("route is required")
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
	input.ModelResolutionMode = strings.TrimSpace(input.ModelResolutionMode)     // swobu:io-string source=domain
	input.ModelRequested = strings.TrimSpace(input.ModelRequested)               // swobu:io-string source=domain
	input.ModelResolved = strings.TrimSpace(input.ModelResolved)                 // swobu:io-string source=domain
	input.WorkspaceRouteModelID = strings.TrimSpace(input.WorkspaceRouteModelID) // swobu:io-string source=domain
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

func (e TrafficEvent) RequestID() RequestID                    { return e.requestID }
func (e TrafficEvent) EventKind() EventKind                    { return e.eventKind }
func (e TrafficEvent) Workspace() string                       { return e.workspace }
func (e TrafficEvent) ClientProtocol() ClientProtocol          { return e.clientProtocol }
func (e TrafficEvent) RequestPath() canonical.NormalizedPath   { return e.requestPath }
func (e TrafficEvent) ClientHandler() ClientHandler            { return e.clientHandler }
func (e TrafficEvent) ClientFamily() ClientFamily              { return e.clientFamily }
func (e TrafficEvent) NormalizedOp() NormalizedOp              { return e.normalizedOp }
func (e TrafficEvent) Route() Route                            { return e.route }
func (e TrafficEvent) BridgeID() string                        { return e.bridgeID }
func (e TrafficEvent) DecisionReason() string                  { return e.decisionReason }
func (e TrafficEvent) AdaptationChain() []string               { return slices.Clone(e.adaptationChain) }
func (e TrafficEvent) Result() ResultClass                     { return e.result }
func (e TrafficEvent) StatusCode() int                         { return e.statusCode }
func (e TrafficEvent) DeliveryKind() delivery.ResultKind       { return e.deliveryKind }
func (e TrafficEvent) CanonicalErrorCode() canonical.ErrorCode { return e.canonicalErrorCode }
func (e TrafficEvent) Timing() Timing                          { return e.timing }
func (e TrafficEvent) AttemptCount() int                       { return e.attemptCount }
func (e TrafficEvent) FallbackRecovered() bool                 { return e.fallbackRecovered }
func (e TrafficEvent) ContinuityRecovered() bool               { return e.continuityRecovered }
func (e TrafficEvent) ContinuityRecoveryTrigger() string       { return e.continuityRecoveryTrigger }
func (e TrafficEvent) ModelResolutionMode() string             { return e.modelResolutionMode }
func (e TrafficEvent) ModelRequested() string                  { return e.modelRequested }
func (e TrafficEvent) ModelResolved() string                   { return e.modelResolved }
func (e TrafficEvent) WorkspaceRouteModelID() string           { return e.workspaceRouteModelID }
func (e TrafficEvent) ProviderSpec() profile.ProviderID        { return e.providerSpec }
func (e TrafficEvent) ProviderModel() string                   { return e.providerModel }
func (e TrafficEvent) TokenUsage() TokenUsage                  { return e.tokenUsage }
func (e TrafficEvent) Mutations() []Mutation {
	return cloneMutations(e.wireMutations)
}
func (e TrafficEvent) ExchangeDiagnostics() []string {
	return slices.Clone(e.exchangeDiagnostics)
}
func (e TrafficEvent) StageReports() []StageReport {
	return cloneStageReports(e.exchangeStageReports)
}
