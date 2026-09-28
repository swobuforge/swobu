// Package executionevidence owns the content-free, immutable lifecycle facts
// for admitted client exchanges and their external executable operations.
// Evidence describes execution; it never grants or changes execution authority.
package executionevidence

import (
	"errors"
	"time"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/clientfamily"
	"github.com/swobuforge/swobu/internal/domain/execution"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/routing"
)

// Event is a closed union of lifecycle and managed-admission facts.
type Event interface{ executionEvidenceEvent() }

type ExchangeStarted struct {
	id        execution.ExchangeID
	access    accessidentity.ID
	workspace routing.WorkspaceSlug
	started   time.Time
	client    clientfamily.Family
	family    protocolkind.ProtocolKind
	operation canonical.NormalizedPath
}

func (ExchangeStarted) executionEvidenceEvent() {}

func NewExchangeStarted(id execution.ExchangeID, access accessidentity.ID, workspace routing.WorkspaceSlug, at time.Time, client clientfamily.Family, family protocolkind.ProtocolKind, operation canonical.NormalizedPath) (ExchangeStarted, error) {
	if !id.Valid() || !access.Valid() || workspace.String() == "" || at.IsZero() || !client.Valid() || operation == "" {
		return ExchangeStarted{}, errors.New("exchange start requires identity, authority, time and client operation")
	}
	if _, err := protocolkind.ParseProtocolKind(family.String()); err != nil {
		return ExchangeStarted{}, err
	}
	return ExchangeStarted{id, access, workspace, at, client, family, operation}, nil
}

func (e ExchangeStarted) ID() execution.ExchangeID                  { return e.id }
func (e ExchangeStarted) AccessID() accessidentity.ID               { return e.access }
func (e ExchangeStarted) Workspace() routing.WorkspaceSlug          { return e.workspace }
func (e ExchangeStarted) StartedAt() time.Time                      { return e.started }
func (e ExchangeStarted) ClientFamily() clientfamily.Family         { return e.client }
func (e ExchangeStarted) ClientProtocol() protocolkind.ProtocolKind { return e.family }
func (e ExchangeStarted) Operation() canonical.NormalizedPath       { return e.operation }

type ModelPurpose string

const (
	ExchangeAttempt        ModelPurpose = "exchange_attempt"
	TargetCharacterization ModelPurpose = "target_characterization"
)

// ModelResource records an exact backend generation, never its endpoint,
// credentials or request/response contents.
type ModelResource struct {
	route    routing.RouteName
	target   routing.TargetID
	version  routing.TargetVersion
	provider routing.Provider
	protocol protocolkind.ProtocolKind
	model    routing.UpstreamModel
	purpose  ModelPurpose
}

func NewModelResource(route routing.RouteName, target routing.TargetID, version routing.TargetVersion, provider routing.Provider, protocol protocolkind.ProtocolKind, model routing.UpstreamModel, purpose ModelPurpose) (ModelResource, error) {
	if route.String() == "" || target.String() == "" || version == 0 || provider == "" || model.String() == "" {
		return ModelResource{}, errors.New("model invocation requires an exact backend generation")
	}
	if _, err := protocolkind.ParseProtocolKind(protocol.String()); err != nil {
		return ModelResource{}, err
	}
	if purpose != ExchangeAttempt && purpose != TargetCharacterization {
		return ModelResource{}, errors.New("model invocation purpose is invalid")
	}
	return ModelResource{route, target, version, provider, protocol, model, purpose}, nil
}

func (m ModelResource) Route() routing.RouteName            { return m.route }
func (m ModelResource) Target() routing.TargetID            { return m.target }
func (m ModelResource) Version() routing.TargetVersion      { return m.version }
func (m ModelResource) Provider() routing.Provider          { return m.provider }
func (m ModelResource) Protocol() protocolkind.ProtocolKind { return m.protocol }
func (m ModelResource) Model() routing.UpstreamModel        { return m.model }
func (m ModelResource) Purpose() ModelPurpose               { return m.purpose }

type MCPResource struct{ source, tool canonical.ToolKey }

func NewMCPResource(source, tool canonical.ToolKey) (MCPResource, error) {
	if source.IsZero() || tool.IsZero() {
		return MCPResource{}, errors.New("MCP invocation requires source and tool keys")
	}
	return MCPResource{source, tool}, nil
}

func (m MCPResource) Source() canonical.ToolKey { return m.source }
func (m MCPResource) Tool() canonical.ToolKey   { return m.tool }

type InvocationStartedEvent interface {
	executionEvidenceEvent()
	ID() execution.InvocationID
	ExchangeID() execution.ExchangeID
	Ordinal() uint64
	StartedAt() time.Time
}

type ModelInvocationStarted struct {
	id      execution.InvocationID
	started time.Time
	model   ModelResource
}

var _ Event = ModelInvocationStarted{}
var _ InvocationStartedEvent = ModelInvocationStarted{}

func (ModelInvocationStarted) executionEvidenceEvent() {}

func NewModelInvocationStarted(id execution.InvocationID, at time.Time, resource ModelResource) (ModelInvocationStarted, error) {
	if !id.Valid() || at.IsZero() || resource.target.String() == "" {
		return ModelInvocationStarted{}, errors.New("model invocation start is incomplete")
	}
	return ModelInvocationStarted{id: id, started: at, model: resource}, nil
}

type MCPInvocationStarted struct {
	id       execution.InvocationID
	started  time.Time
	resource MCPResource
}

func (MCPInvocationStarted) executionEvidenceEvent() {}

var _ Event = MCPInvocationStarted{}
var _ InvocationStartedEvent = MCPInvocationStarted{}

func NewMCPInvocationStarted(id execution.InvocationID, at time.Time, resource MCPResource) (MCPInvocationStarted, error) {
	if !id.Valid() || at.IsZero() || resource.source.IsZero() {
		return MCPInvocationStarted{}, errors.New("MCP invocation start is incomplete")
	}
	return MCPInvocationStarted{id: id, started: at, resource: resource}, nil
}

func (e ModelInvocationStarted) ID() execution.InvocationID       { return e.id }
func (e ModelInvocationStarted) ExchangeID() execution.ExchangeID { return e.id.ExchangeID() }
func (e ModelInvocationStarted) Ordinal() uint64                  { return e.id.Ordinal() }
func (e ModelInvocationStarted) StartedAt() time.Time             { return e.started }
func (e ModelInvocationStarted) Model() ModelResource             { return e.model }

func (e MCPInvocationStarted) ID() execution.InvocationID       { return e.id }
func (e MCPInvocationStarted) ExchangeID() execution.ExchangeID { return e.id.ExchangeID() }
func (e MCPInvocationStarted) Ordinal() uint64                  { return e.id.Ordinal() }
func (e MCPInvocationStarted) StartedAt() time.Time             { return e.started }
func (e MCPInvocationStarted) MCP() MCPResource                 { return e.resource }

type ModelFailure string

const (
	FailureUnavailable        ModelFailure = "unavailable"
	FailureTimeout            ModelFailure = "timeout"
	FailureRejected           ModelFailure = "rejected"
	FailureTargetUnavailable  ModelFailure = "target_unavailable"
	FailureTargetIncompatible ModelFailure = "target_incompatible"
	FailureInvalidRequest     ModelFailure = "invalid_request"
	FailureInternal           ModelFailure = "internal"
	FailureResponseInvalid    ModelFailure = "response_invalid"
)

func (f ModelFailure) Valid() bool {
	switch f {
	case FailureUnavailable, FailureTimeout, FailureRejected, FailureTargetUnavailable, FailureTargetIncompatible, FailureInvalidRequest, FailureInternal, FailureResponseInvalid:
		return true
	default:
		return false
	}
}

type MCPFailure string

const (
	FailureToolError        MCPFailure = "tool_error"
	FailureMCPTransport     MCPFailure = "mcp_transport_or_protocol"
	FailureResultProjection MCPFailure = "result_projection"
)

func (f MCPFailure) Valid() bool {
	return f == FailureToolError || f == FailureMCPTransport || f == FailureResultProjection
}

type InvocationSettledEvent interface {
	Event
	ID() execution.InvocationID
	SettledAt() time.Time
	Outcome() execution.Outcome
	Occurrence() execution.Occurrence
}

type ModelInvocationSettled struct {
	id         execution.InvocationID
	at         time.Time
	outcome    execution.Outcome
	occurrence execution.Occurrence
	failure    ModelFailure
	usage      canonical.TokenUsage
}

func (ModelInvocationSettled) executionEvidenceEvent() {}

func NewModelInvocationSettled(id execution.InvocationID, at time.Time, outcome execution.Outcome, occurrence execution.Occurrence, failure ModelFailure, usage canonical.TokenUsage) (ModelInvocationSettled, error) {
	if !id.Valid() || at.IsZero() || !outcome.Valid() || !occurrence.Valid() {
		return ModelInvocationSettled{}, errors.New("model invocation settlement is invalid")
	}
	if (outcome == execution.Succeeded && (occurrence != execution.Occurred || failure != "")) || (outcome == execution.Cancelled && failure != "") || (outcome == execution.Failed && !failure.Valid()) {
		return ModelInvocationSettled{}, errors.New("model invocation outcome, occurrence and failure disagree")
	}
	return ModelInvocationSettled{id: id, at: at, outcome: outcome, occurrence: occurrence, failure: failure, usage: usage}, nil
}

func (e ModelInvocationSettled) ID() execution.InvocationID       { return e.id }
func (e ModelInvocationSettled) SettledAt() time.Time             { return e.at }
func (e ModelInvocationSettled) Outcome() execution.Outcome       { return e.outcome }
func (e ModelInvocationSettled) Occurrence() execution.Occurrence { return e.occurrence }
func (e ModelInvocationSettled) Failure() ModelFailure            { return e.failure }
func (e ModelInvocationSettled) Usage() canonical.TokenUsage      { return e.usage }

type MCPInvocationSettled struct {
	id         execution.InvocationID
	at         time.Time
	outcome    execution.Outcome
	occurrence execution.Occurrence
	failure    MCPFailure
}

func (MCPInvocationSettled) executionEvidenceEvent() {}

func NewMCPInvocationSettled(id execution.InvocationID, at time.Time, outcome execution.Outcome, occurrence execution.Occurrence, failure MCPFailure) (MCPInvocationSettled, error) {
	if !id.Valid() || at.IsZero() || !outcome.Valid() || !occurrence.Valid() || (outcome == execution.Succeeded && (occurrence != execution.Occurred || failure != "")) || (outcome == execution.Cancelled && failure != "") || (outcome == execution.Failed && !failure.Valid()) {
		return MCPInvocationSettled{}, errors.New("MCP invocation settlement is invalid")
	}
	return MCPInvocationSettled{id: id, at: at, outcome: outcome, occurrence: occurrence, failure: failure}, nil
}

func (e MCPInvocationSettled) ID() execution.InvocationID       { return e.id }
func (e MCPInvocationSettled) SettledAt() time.Time             { return e.at }
func (e MCPInvocationSettled) Outcome() execution.Outcome       { return e.outcome }
func (e MCPInvocationSettled) Occurrence() execution.Occurrence { return e.occurrence }
func (e MCPInvocationSettled) Failure() MCPFailure              { return e.failure }

type ExchangeSettled struct {
	id     execution.ExchangeID
	at     time.Time
	result delivery.ResultKind
	status int
	code   canonical.ErrorCode
}

func (ExchangeSettled) executionEvidenceEvent() {}

func NewExchangeSettled(id execution.ExchangeID, at time.Time, result delivery.ResultKind, status int, code canonical.ErrorCode) (ExchangeSettled, error) {
	if !id.Valid() || at.IsZero() || !delivery.ValidResultKind(result) || (status != 0 && (status < 100 || status > 599)) || (code != "" && !canonical.ValidErrorCode(code)) {
		return ExchangeSettled{}, errors.New("exchange settlement is invalid")
	}
	return ExchangeSettled{id: id, at: at, result: result, status: status, code: code}, nil
}

func (e ExchangeSettled) ID() execution.ExchangeID       { return e.id }
func (e ExchangeSettled) SettledAt() time.Time           { return e.at }
func (e ExchangeSettled) Result() delivery.ResultKind    { return e.result }
func (e ExchangeSettled) Status() int                    { return e.status }
func (e ExchangeSettled) ErrorCode() canonical.ErrorCode { return e.code }

type AdmissionReason string

const (
	InvalidCredential    AdmissionReason = "invalid_credential"
	AmbiguousCredentials AdmissionReason = "ambiguous_credentials"
	WorkspaceMismatch    AdmissionReason = "workspace_mismatch"
)

type ManagedAccessAdmissionRejected struct {
	at        time.Time
	workspace routing.WorkspaceSlug
	access    accessidentity.ID
	reason    AdmissionReason
}

func (ManagedAccessAdmissionRejected) executionEvidenceEvent() {}

func NewManagedAccessAdmissionRejected(at time.Time, workspace routing.WorkspaceSlug, access accessidentity.ID, reason AdmissionReason) (ManagedAccessAdmissionRejected, error) {
	if at.IsZero() || (reason != InvalidCredential && reason != AmbiguousCredentials && reason != WorkspaceMismatch) || (access.Valid() && (reason != WorkspaceMismatch || !access.Managed())) || (reason == WorkspaceMismatch && (!access.Managed() || workspace.String() == "")) {
		return ManagedAccessAdmissionRejected{}, errors.New("managed admission rejection is invalid")
	}
	return ManagedAccessAdmissionRejected{at, workspace, access, reason}, nil
}

func (e ManagedAccessAdmissionRejected) ObservedAt() time.Time            { return e.at }
func (e ManagedAccessAdmissionRejected) Workspace() routing.WorkspaceSlug { return e.workspace }
func (e ManagedAccessAdmissionRejected) AccessID() accessidentity.ID      { return e.access }
func (e ManagedAccessAdmissionRejected) Reason() AdmissionReason          { return e.reason }
