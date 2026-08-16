package adapters

import (
	"context"
	"fmt"
	"strings"
	"time"

	operatorclient "github.com/swobuforge/swobu/internal/app/operator/client"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

func (a *LiveOperatorAdapter) ListActivity(ctx context.Context, request ports.ListActivityRequest) (readmodel.ActivityReadModel, error) {
	scope := "all"
	if request.WorkspaceID != "" {
		scope = "workspace:" + string(request.WorkspaceID)
	}
	projection, err := a.client.Status(ctx, scope)
	if err != nil {
		return readmodel.ActivityReadModel{}, adapterFailure(fmt.Sprintf("list activity %q", scope), err)
	}
	return activityFromProjection(projection, request.Limit), nil
}

func (a *LiveOperatorAdapter) activityForWorkspace(ctx context.Context, workspaceID readmodel.WorkspaceID) (readmodel.ActivityReadModel, error) {
	return a.ListActivity(ctx, ports.ListActivityRequest{WorkspaceID: workspaceID, Limit: 5})
}

func activityFromProjection(projection operatorclient.StatusProjection, limit int) readmodel.ActivityReadModel {
	rows := make([]readmodel.ActivityRowReadModel, 0, len(projection.RecentTraffic))
	for _, traffic := range projection.RecentTraffic {
		rows = append(rows, activityRowFromTraffic(traffic))
		if limit > 0 && len(rows) >= limit {
			break
		}
	}
	activity := readmodel.ActivityReadModel{Rows: rows}
	if len(rows) > 0 {
		latest := rows[0]
		activity.Latest = &latest
	}
	return activity
}

func activityRowFromTraffic(row operatorclient.RecentTrafficRow) readmodel.ActivityRowReadModel {
	duration, durationKnown := trafficDuration(row)
	return readmodel.ActivityRowReadModel{
		ID:             readmodel.ActivityID(row.RequestID),
		ObservedAt:     strings.TrimSpace(row.ObservedAt),     // swobu:io-string source=boundary
		RequestedModel: strings.TrimSpace(row.ModelRequested), // swobu:io-string source=boundary
		// The evidence layer owns client-handler normalization; Cockpit reads the
		// canonical handler label directly and does not re-guess from other fields.
		ClientLabel: row.ClientHandler,
		// WorkspaceRouteModelID is the route selected by routing. Keep the lower-
		// level Route only as a compatibility fallback for older evidence.
		RouteID:       readmodel.RouteID(firstNonEmpty(row.WorkspaceRouteModelID, row.Route)),
		RouteLabel:    firstNonEmpty(row.WorkspaceRouteModelID, row.Route),
		ProviderSpec:  row.ProviderSpec,
		ProviderModel: row.ProviderModel,
		Status:        activityStatus(row.Result, row.StatusCode),
		HTTPStatus:    row.StatusCode,
		AttemptCount:  row.AttemptCount,
		Duration:      duration,
		DurationKnown: durationKnown,
		Error:         row.StatusCode >= 400 || row.Result == "backend_error" || row.Result == "swobu_error",
		TokensIn:      inputTokens(row.TokenUsage),
		TokensOut:     outputTokens(row.TokenUsage),
	}
}

func activityStatus(result string, statusCode int) readmodel.ActivityStatus {
	result = strings.ToLower(strings.TrimSpace(result)) // swobu:io-string source=boundary
	switch {
	case result == "in_progress":
		return readmodel.ActivityPending
	case statusCode >= 400:
		return readmodel.ActivityFailed
	case result == "", result == "success":
		return readmodel.ActivitySucceeded
	case result == "canceled", result == "cancelled":
		return readmodel.ActivityCanceled
	default:
		return readmodel.ActivityFailed
	}
}

func trafficDuration(row operatorclient.RecentTrafficRow) (time.Duration, bool) {
	// Timing knownness is operator evidence: absent timing must not become a
	// measured zero-duration request at the Cockpit boundary.
	if row.Timing == nil || row.Timing.DurMillis == nil {
		return 0, false
	}
	return time.Duration(*row.Timing.DurMillis) * time.Millisecond, true
}

func inputTokens(usage *operatorclient.RecentTrafficTokenUseRecord) int {
	if usage != nil && usage.InputTokens != nil {
		return *usage.InputTokens
	}
	return 0
}

func outputTokens(usage *operatorclient.RecentTrafficTokenUseRecord) int {
	if usage != nil && usage.OutputTokens != nil {
		return *usage.OutputTokens
	}
	return 0
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" { // swobu:io-string source=boundary
			return trimmed
		}
	}
	return ""
}
