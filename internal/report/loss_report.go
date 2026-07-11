package report

import "github.com/swobuforge/swobu/internal/delta"

type Stage string

const (
	StageClientHTTPIn    Stage = "client_request.transport_in"
	StageClientWireIn    Stage = "client_request.wire_in"
	StageSemanticRequest Stage = "semantic.request"

	StageRequestDocumentOut Stage = "provider_request.wire_out"
	StageProviderHTTPOut    Stage = "provider_request.transport_out"

	StageProviderHTTPIn    Stage = "provider_response.transport_in"
	StageRequestDocumentIn Stage = "provider_response.wire_in"
	StageSemanticEvents    Stage = "semantic.response_events"

	StageClientWireOut Stage = "client_response.wire_out"
	StageClientHTTPOut Stage = "client_response.transport_out"
)

type StageReport struct {
	Stage   string   `json:"stage"`
	Carrier string   `json:"carrier"`
	Applied []string `json:"applied"`
	Mutated bool     `json:"mutated"`
}

type LossKind string

const (
	LossUnsupportedField            LossKind = "unsupported_field"
	LossUnrepresentableTool         LossKind = "unrepresentable_tool"
	LossUnrepresentableContentPart  LossKind = "unrepresentable_content_part"
	LossProviderPrivateStateMissing LossKind = "provider_private_state_missing"
)

type LossClass string

const (
	LossClassRepresentational LossClass = "representational"
	LossClassSemantic         LossClass = "semantic"
	LossClassFatal            LossClass = "fatal"
)

type Severity string

const (
	SeverityNotice  Severity = "notice"
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
)

type PathKind = delta.MutationPathKind
type Path = delta.MutationPathRecord

const (
	PathKindJSONPointer = delta.PathKindJSONPointer
	PathKindFramePath   = delta.PathKindFramePath
	PathKindEventPath   = delta.PathKindEventPath
	PathKindSemantic    = delta.PathKindSemantic
	PathKindState       = delta.PathKindState
)

type ReasonCode string

const (
	ReasonTargetRejectsUnknownField ReasonCode = "target_rejects_unknown_field"
	ReasonTargetLacksToolForm       ReasonCode = "target_lacks_tool_form"
	ReasonTargetLacksContentPart    ReasonCode = "target_lacks_content_part"
	ReasonDuplicateUsageReport      ReasonCode = "duplicate_usage_report"
	ReasonMissingTerminalEvent      ReasonCode = "missing_terminal_event"
	ReasonOpaqueStateRequired       ReasonCode = "opaque_state_required"
	ReasonInvalidEventLifecycle     ReasonCode = "invalid_event_lifecycle"
	ReasonTransportDeliveryFailed   ReasonCode = "transport_delivery_failed"
)

type Loss struct {
	Field      string     `json:"field"`
	Kind       LossKind   `json:"kind"`
	Class      LossClass  `json:"class,omitempty"`
	Path       Path       `json:"path,omitempty"`
	ReasonCode ReasonCode `json:"reason_code,omitempty"`
	Reason     string     `json:"reason"`
	Severity   Severity   `json:"severity"`
}

func (l Loss) Empty() bool {
	return l.Field == "" && l.Kind == "" && l.Class == "" && l.Path == (Path{}) && l.ReasonCode == "" && l.Reason == "" && l.Severity == ""
}

type Notice struct {
	Code   string `json:"code"`
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

func (n Notice) Empty() bool {
	return n.Code == "" && n.Field == "" && n.Reason == ""
}

type Evidence struct {
	Code   string `json:"code"`
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

type Mutation = delta.MutationRecord

type ExchangeReport struct {
	ExchangeID string        `json:"exchange_id"`
	Stages     []StageReport `json:"stages"`
	Losses     []Loss        `json:"losses"`
	Notices    []Notice      `json:"notices"`
	Evidence   []Evidence    `json:"evidence"`
}
