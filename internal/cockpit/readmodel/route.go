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
	RouteBlocked
	RouteDegraded
)

// TargetReadModel is the expanded route target detail shown under a route row.
type TargetReadModel struct {
	ID               TargetID
	Name             string
	Provider         string
	Model            string
	ProviderProtocol string
	BaseURL          string
	AuthMode         string
	AuthHeader       string
	CredentialRef    string
	Rank             int
	Weight           int
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
	return r.Enabled && len(r.Targets) > 0
}

// StepCount returns the number of distinct ranks (steps) in the route.
// Targets with the same Rank are part of the same step.
func (r RouteReadModel) StepCount() int {
	seen := make(map[int]struct{})
	for _, t := range r.Targets {
		seen[t.Rank] = struct{}{}
	}
	return len(seen)
}

// HasBalancedStep reports whether any step has more than one target.
func (r RouteReadModel) HasBalancedStep() bool {
	counts := make(map[int]int)
	for _, t := range r.Targets {
		counts[t.Rank]++
		if counts[t.Rank] > 1 {
			return true
		}
	}
	return false
}

// RowValue derives the bounded route row value used by Cockpit sections.
// Grammar is mechanical per the structural model route RFC:
//
//	targets == 0                     → "incomplete · no targets"
//	steps == 1 && targets == 1       → "1 target"
//	steps == 1 && targets > 1        → "N balanced targets"
//	steps > 1 && one target/step     → "N fallback steps"
//	steps > 1                         → "N steps · M targets"
func (r RouteReadModel) RowValue() string {
	targets := len(r.Targets)
	steps := r.StepCount()

	var base string
	switch {
	case targets == 0:
		base = "incomplete · no targets"
	case steps == 1 && targets == 1:
		base = "1 target"
	case steps == 1 && targets > 1:
		base = fmt.Sprintf("%d balanced targets", targets)
	case steps > 1 && !r.HasBalancedStep():
		base = fmt.Sprintf("%d fallback steps", steps)
	default:
		base = fmt.Sprintf("%d steps · %d targets", steps, targets)
	}

	switch r.State {
	case RouteBlocked:
		return base + " · blocked " + r.primaryDiagnosticLabel()
	case RouteDegraded:
		return base + " · degraded " + r.primaryDiagnosticLabel()
	case RouteDisabled:
		return base + " · disabled"
	default:
		if r.Default {
			return "default · " + base
		}
		return base
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
