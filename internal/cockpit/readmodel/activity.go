package readmodel

import (
	"time"
)

// ActivityID is the stable Cockpit identifier for one observed request row.
type ActivityID string

// ActivityReadModel is the workspace activity projection shown by the activity
// section.
type ActivityReadModel struct {
	Latest *ActivityRowReadModel
	Rows   []ActivityRowReadModel
}

// ActivityRowReadModel is one observed request row plus optional expanded
// inspection detail.
type ActivityRowReadModel struct {
	ID ActivityID
	// ObservedAt preserves the daemon's observation label as display text.
	// Cockpit does not synthesize a timestamp from partial clock data.
	ObservedAt string
	// ClientLabel is the normalized client handler shown in the activity row.
	ClientLabel string
	RouteID     RouteID
	RouteLabel  string
	// ProviderSpec and ProviderModel describe the target selected for this
	// observed execution. They are historical evidence, not a live config lookup.
	ProviderSpec  string
	ProviderModel string
	Status        ActivityStatus
	HTTPStatus    int
	// AttemptCount is the number of provider calls made while resolving this
	// request. A value greater than one is operator-visible failover evidence.
	AttemptCount int
	// DurationKnown distinguishes absent timing from a measured zero-duration
	// request. Pending rows and events without terminal timing leave it false.
	Duration      time.Duration
	DurationKnown bool
	Error         bool
	Attempts      []ActivityAttemptReadModel
	TokensIn      int
	TokensOut     int
}

// ActivityStatus is the typed result state used by copy and styling.
type ActivityStatus int

const (
	ActivityPending ActivityStatus = iota
	ActivitySucceeded
	ActivityFailed
	ActivityCanceled
)

// ActivityAttemptReadModel is one route attempt shown in expanded activity.
type ActivityAttemptReadModel struct {
	Label  string
	Rank   int
	Result ActivityAttemptResult
}

// ActivityAttemptResult is the bounded attempt outcome vocabulary for Cockpit.
type ActivityAttemptResult int

const (
	ActivityAttemptSucceeded ActivityAttemptResult = iota
	ActivityAttemptFailed
	ActivityAttemptSkipped
)

// IsEmpty reports whether there is no activity to render.
func (a ActivityReadModel) IsEmpty() bool {
	return a.Latest == nil && len(a.Rows) == 0
}

// LatestRow returns the explicit latest row or derives it from Rows.
func (a ActivityReadModel) LatestRow() (ActivityRowReadModel, bool) {
	if a.Latest != nil {
		return *a.Latest, true
	}
	if len(a.Rows) == 0 {
		return ActivityRowReadModel{}, false
	}
	return a.Rows[0], true
}
