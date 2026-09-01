package readmodel

import (
	"fmt"
	"time"
)

type ShareReadModel struct {
	Hostname  string
	ExpiresAt time.Time
	Never     bool
}

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
	Tiers       []TierReadModel
	Diagnostics []RouteDiagnosticReadModel
	ActivityID  ActivityID
	Share       *ShareReadModel
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
	ZAIAccess        string
	BaseURL          string
	AuthHeader       string
	CredentialRef    string
	// BedrockRegion carries the durable Bedrock signing region so the Cockpit
	// form authors region directly from a persisted fact instead of parsing it
	// back out of the endpoint URL. Empty for every non-Bedrock target and for
	// a Bedrock target that has no authored region.
	BedrockRegion string
}

// TierReadModel is one structural fallback tier. Position, not a persisted
// ordinal, determines whether it is primary or fallback.
type TierReadModel struct{ Targets []TargetReadModel }

func (r RouteReadModel) TargetCount() int {
	total := 0
	for _, tier := range r.Tiers {
		total += len(tier.Targets)
	}
	return total
}
func (r RouteReadModel) AllTargets() []TargetReadModel {
	var out []TargetReadModel
	for _, tier := range r.Tiers {
		out = append(out, tier.Targets...)
	}
	return out
}
func (r RouteReadModel) TargetTier(id TargetID) (int, bool) {
	for i, tier := range r.Tiers {
		for _, target := range tier.Targets {
			if target.ID == id {
				return i, true
			}
		}
	}
	return 0, false
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
	return r.Enabled && r.TargetCount() > 0
}

func (r RouteReadModel) TierCount() int { return len(r.Tiers) }

// HasBalancedStep reports whether any step has more than one target.
func (r RouteReadModel) HasBalancedTier() bool {
	for _, tier := range r.Tiers {
		if len(tier.Targets) > 1 {
			return true
		}
	}
	return false
}

// RowValue derives the bounded route row value used by Cockpit sections.
// Grammar is mechanical per the structural model route RFC:
//
//	targets == 0                     → "no targets"
//	steps == 1 && targets == 1       → "1 target"
//	steps == 1 && targets > 1        → "N balanced targets"
//	steps > 1 && one target/step     → "N fallback steps"
//	steps > 1                         → "N steps · M targets"
func (r RouteReadModel) RowValue() string {
	targets := r.TargetCount()
	tiers := r.TierCount()

	var base string
	switch {
	case targets == 0:
		base = "no targets"
	case tiers == 1 && targets == 1:
		base = "1 target"
	case tiers == 1 && targets > 1:
		base = fmt.Sprintf("%d balanced targets", targets)
	case tiers > 1 && !r.HasBalancedTier():
		base = fmt.Sprintf("%d fallback tiers", tiers-1)
	default:
		base = fmt.Sprintf("%d tiers · %d targets", tiers, targets)
	}

	switch r.State {
	case RouteBlocked:
		return base + " · blocked " + r.primaryDiagnosticLabel()
	case RouteDegraded:
		return base + " · degraded " + r.primaryDiagnosticLabel()
	case RouteDisabled:
		return base + " · disabled"
	default:
		if r.Default && targets > 0 {
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
