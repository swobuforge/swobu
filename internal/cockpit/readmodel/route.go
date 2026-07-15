package readmodel

import "fmt"

// RouteID is the stable Cockpit identifier for a client-visible model name.
type RouteID string

// TargetID is the stable Cockpit identifier for one route target.
type TargetID string

// RouteReadModel is the canonical Cockpit route row and detail projection.
//
// A route represents the model name a client can request. Its state and plan
// kind are Cockpit display taxonomy; domain routing remains owned by the
// adapter and core packages that build this projection.
type RouteReadModel struct {
	ID          RouteID
	ModelName   string
	State       RouteState
	PlanKind    RoutePlanKind
	Default     bool
	Enabled     bool
	Targets     []TargetReadModel
	Diagnostics []RouteDiagnosticReadModel
	ActivityID  ActivityID
}

// RouteState is the operator-facing route status used by row copy and styling.
type RouteState int

const (
	RouteNormal RouteState = iota
	RouteDisabled
	RouteIncomplete
	RouteBlocked
	RouteDegraded
)

// RoutePlanKind describes the target plan shape without exposing domain
// routing internals to section renderers.
type RoutePlanKind int

const (
	RoutePlanSingle RoutePlanKind = iota
	RoutePlanRanked
	RoutePlanWeighted
)

// TargetReadModel is the expanded route target detail shown under a route row.
type TargetReadModel struct {
	ID            TargetID
	Name          string
	Provider      string
	Model         string
	BaseURL       string
	CredentialRef string
	Rank          int
	Weight        int
}

// RouteDiagnosticReadModel is a typed exceptional-state detail for a route.
type RouteDiagnosticReadModel struct {
	Kind       RouteDiagnosticKind
	StatusCode int
	Count      int
}

// RouteDiagnosticKind identifies the exceptional condition a route row may
// disclose.
type RouteDiagnosticKind int

const (
	RouteDiagnosticUnknown RouteDiagnosticKind = iota
	RouteDiagnosticRateLimited
	RouteDiagnosticUnreachable
	RouteDiagnosticUnauthorized
	RouteDiagnosticNoTargets
)

// IsClientVisible reports the design-system rule for routes visible to clients.
func (r RouteReadModel) IsClientVisible() bool {
	return r.Enabled && r.State != RouteIncomplete && len(r.Targets) > 0
}

// RowValue derives the bounded route row value used by Cockpit sections.
func (r RouteReadModel) RowValue() string {
	switch r.State {
	case RouteIncomplete:
		return "incomplete · no targets"
	case RouteBlocked:
		return routePlanLabel(r.PlanKind, len(r.Targets)) + " · blocked " + r.primaryDiagnosticLabel()
	case RouteDegraded:
		return routePlanLabel(r.PlanKind, len(r.Targets)) + " · degraded " + r.primaryDiagnosticLabel()
	case RouteDisabled:
		return routePlanLabel(r.PlanKind, len(r.Targets)) + " · disabled"
	default:
		if r.Default {
			return "default · " + routePlanLabel(r.PlanKind, len(r.Targets))
		}
		return routePlanLabel(r.PlanKind, len(r.Targets))
	}
}

func (r RouteReadModel) primaryDiagnosticLabel() string {
	if len(r.Diagnostics) == 0 {
		return "unknown"
	}
	d := r.Diagnostics[0]
	switch d.Kind {
	case RouteDiagnosticRateLimited:
		if d.StatusCode > 0 && d.Count > 0 {
			return fmt.Sprintf("%d x%d", d.StatusCode, d.Count)
		}
		if d.StatusCode > 0 {
			return fmt.Sprint(d.StatusCode)
		}
		return "rate limited"
	case RouteDiagnosticUnreachable:
		return "unreachable"
	case RouteDiagnosticUnauthorized:
		return "unauthorized"
	case RouteDiagnosticNoTargets:
		return "no targets"
	default:
		return "unknown"
	}
}

func targetCountLabel(n int) string {
	switch n {
	case 0:
		return "no targets"
	case 1:
		return "1 target"
	default:
		return fmt.Sprintf("%d targets", n)
	}
}

func routePlanLabel(kind RoutePlanKind, targets int) string {
	if kind == RoutePlanWeighted && targets > 1 {
		return fmt.Sprintf("%d weighted", targets)
	}
	return targetCountLabel(targets)
}
