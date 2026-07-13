package routing

import "time"

// Trace is an immutable request-scoped event log.
type Trace struct {
	ExchangeID string
	Workspace  string
	RouteModel string
	Events     []TraceEvent
}

// TraceEvent is one step in the request trace.
type TraceEvent struct {
	Time     time.Time
	Kind     TraceKind
	TargetID string // empty for non-target events
	Rank     int    // 0 for non-target events
	Weight   int    // 0 for non-target events
	Reason   string // e.g. filter reason, failure class
	Detail   string // e.g. cooldown duration, status code
}

// TraceKind classifies what happened during routing.
type TraceKind string

const (
	TraceRouteResolved  TraceKind = "route_resolved"
	TraceTargetFiltered TraceKind = "target_filtered"
	TracePlanBuilt      TraceKind = "plan_built"
	TraceAttempt        TraceKind = "attempt"
	TraceFailure        TraceKind = "failure"
	TraceCooldown       TraceKind = "cooldown"
	TraceSuccess        TraceKind = "success"
	TraceFinalFailure   TraceKind = "final_failure"
)

// RecordRouteResolved appends a route-resolved event to the trace.
func (t *Trace) RecordRouteResolved(route string) {
	t.Events = append(t.Events, TraceEvent{
		Time:   time.Now(),
		Kind:   TraceRouteResolved,
		Detail: route,
	})
}

// RecordTargetFiltered appends a filtered-target event with its reason.
func (t *Trace) RecordTargetFiltered(targetID string, reason FilterReason, detail string) {
	t.Events = append(t.Events, TraceEvent{
		Time:     time.Now(),
		Kind:     TraceTargetFiltered,
		TargetID: targetID,
		Reason:   string(reason),
		Detail:   detail,
	})
}

// RecordPlanBuilt appends a plan-built event with the attempt plan description.
func (t *Trace) RecordPlanBuilt(detail string) {
	t.Events = append(t.Events, TraceEvent{
		Time:   time.Now(),
		Kind:   TracePlanBuilt,
		Detail: detail,
	})
}

// RecordAttempt appends an attempt-start event.
func (t *Trace) RecordAttempt(targetID string, rank int) {
	t.Events = append(t.Events, TraceEvent{
		Time:     time.Now(),
		Kind:     TraceAttempt,
		TargetID: targetID,
		Rank:     rank,
	})
}

// RecordFailure appends a failure event with failure class and stream-start status.
func (t *Trace) RecordFailure(targetID string, class FailureClass, streamStarted bool) {
	detail := string(class)
	if streamStarted {
		detail += " after stream started"
	} else {
		detail += " before stream"
	}
	t.Events = append(t.Events, TraceEvent{
		Time:     time.Now(),
		Kind:     TraceFailure,
		TargetID: targetID,
		Reason:   string(class),
		Detail:   detail,
	})
}

// RecordCooldown appends a cooldown-mark event with TTL.
func (t *Trace) RecordCooldown(targetID string, class FailureClass, ttl time.Duration) {
	t.Events = append(t.Events, TraceEvent{
		Time:     time.Now(),
		Kind:     TraceCooldown,
		TargetID: targetID,
		Reason:   string(class),
		Detail:   ttl.String(),
	})
}

// RecordSuccess appends a success event.
func (t *Trace) RecordSuccess(targetID string) {
	t.Events = append(t.Events, TraceEvent{
		Time:     time.Now(),
		Kind:     TraceSuccess,
		TargetID: targetID,
	})
}

// RecordFinalFailure appends the terminal failure event.
func (t *Trace) RecordFinalFailure(reason string) {
	t.Events = append(t.Events, TraceEvent{
		Time:   time.Now(),
		Kind:   TraceFinalFailure,
		Detail: reason,
	})
}
