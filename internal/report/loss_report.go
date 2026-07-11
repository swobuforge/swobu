package report

import "fmt"

type Stage string

const (
	StageClientHTTPIn    Stage = "client_http_in"
	StageClientWireIn    Stage = "client_wire_in"
	StageSemanticRequest Stage = "semantic_request"

	StageProviderWireOut Stage = "provider_wire_out"
	StageProviderHTTPOut Stage = "provider_http_out"

	StageProviderHTTPIn Stage = "provider_http_in"
	StageProviderWireIn Stage = "provider_wire_in"
	StageSemanticEvents Stage = "semantic_events"

	StageClientWireOut Stage = "client_wire_out"
	StageClientHTTPOut Stage = "client_http_out"
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

type Severity string

const (
	SeverityNotice  Severity = "notice"
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
)

type Loss struct {
	Field    string   `json:"field"`
	Kind     LossKind `json:"kind"`
	Reason   string   `json:"reason"`
	Severity Severity `json:"severity"`
}

func (l Loss) Empty() bool {
	return l.Field == "" && l.Kind == "" && l.Reason == "" && l.Severity == ""
}

func ValidateLoss(loss Loss) error {
	if loss.Kind == "" {
		return fmt.Errorf("projection loss kind is required")
	}
	if loss.Severity != SeverityNotice && loss.Severity != SeverityWarning && loss.Severity != SeverityError {
		return fmt.Errorf("projection loss severity is invalid")
	}
	if loss.Kind == LossUnrepresentableTool && loss.Severity == SeverityNotice {
		return fmt.Errorf("unrepresentable tool loss cannot be notice")
	}
	return nil
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

type Mutation struct {
	Leg           string   `json:"leg"`
	Transform     string   `json:"transform"`
	Changed       bool     `json:"changed"`
	ChangedFields []string `json:"changed_fields,omitempty"`
}

func (m Mutation) HasChanges() bool {
	return m.Changed
}

type ExchangeReport struct {
	ExchangeID string        `json:"exchange_id"`
	Stages     []StageReport `json:"stages"`
	Losses     []Loss        `json:"losses"`
	Notices    []Notice      `json:"notices"`
	Evidence   []Evidence    `json:"evidence"`
}
