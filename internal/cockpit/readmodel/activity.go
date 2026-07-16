package readmodel

import (
	"fmt"
	"strings"
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
	ObservedAt   string
	ClientLabel  string
	RouteID      RouteID
	RouteLabel   string
	Status       ActivityStatus
	HTTPStatus   int
	Duration     time.Duration
	Error        bool
	ResolvedName string
	Model        string
	Attempts     []ActivityAttemptReadModel
	TokensIn     int
	TokensOut    int
}

// ActivityStatus is the typed result state used by copy and styling.
type ActivityStatus int

const (
	ActivitySucceeded ActivityStatus = iota
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

// RowValue derives the compact activity row used by the activity section.
func (a ActivityRowReadModel) RowValue() string {
	route := a.RouteLabel
	if route == "" {
		route = string(a.RouteID)
	}
	observedAt := strings.TrimSpace(a.ObservedAt) // swobu:io-string source=boundary
	if observedAt == "" {
		observedAt = "unknown"
	}
	status := ""
	if a.HTTPStatus > 0 {
		status = fmt.Sprint(a.HTTPStatus)
	}
	if status == "" {
		switch a.Status {
		case ActivityCanceled:
			status = "canceled"
		case ActivityFailed:
			status = "failed"
		default:
			status = "ok"
		}
	}
	return fmt.Sprintf("%s %s %s %s %s", observedAt, a.ClientLabel, route, status, durationLabel(a.Duration))
}

func durationLabel(d time.Duration) string {
	if d <= 0 {
		return "0ms"
	}
	return fmt.Sprintf("%dms", d.Milliseconds())
}
