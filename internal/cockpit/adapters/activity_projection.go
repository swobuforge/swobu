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
		scope = "endpoint:" + string(request.WorkspaceID)
	}
	projection, err := a.client.Status(ctx, scope)
	if err != nil {
		return readmodel.ActivityReadModel{}, adapterFailure(fmt.Sprintf("list activity %q", scope), err)
	}
	return activityFromProjection(projection, request.Limit), nil
}

func (a *LiveOperatorAdapter) GetActivity(ctx context.Context, id readmodel.ActivityID) (readmodel.ActivityRowReadModel, error) {
	activity, err := a.ListActivity(ctx, ports.ListActivityRequest{})
	if err != nil {
		return readmodel.ActivityRowReadModel{}, adapterFailure(fmt.Sprintf("get activity %q", id), err)
	}
	for _, row := range activity.Rows {
		if row.ID == id {
			return row, nil
		}
	}
	return readmodel.ActivityRowReadModel{}, fmt.Errorf("get activity %q: activity row could not be resolved", id)
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
	return readmodel.ActivityRowReadModel{
		ID:           readmodel.ActivityID(row.RequestID),
		ObservedAt:   strings.TrimSpace(row.ObservedAt), // swobu:io-string source=boundary
		ClientLabel:  firstNonEmpty(row.ClientHandler, row.ClientFamily, row.ClientProtocol),
		RouteID:      readmodel.RouteID(row.Route),
		RouteLabel:   row.Route,
		Status:       activityStatus(row.Result, row.StatusCode),
		HTTPStatus:   row.StatusCode,
		Duration:     trafficDuration(row),
		Error:        row.StatusCode >= 400 || row.Result == "backend_error" || row.Result == "swobu_error",
		ResolvedName: row.ModelResolved,
		Model:        firstNonEmpty(row.ModelResolved, row.ModelRequested),
		TokensIn:     inputTokens(row.TokenUsage),
		TokensOut:    outputTokens(row.TokenUsage),
	}
}

func activityStatus(result string, statusCode int) readmodel.ActivityStatus {
	result = strings.ToLower(strings.TrimSpace(result)) // swobu:io-string source=boundary
	switch {
	case statusCode >= 400:
		return readmodel.ActivityFailed
	case result == "", result == "success":
		return readmodel.ActivitySucceeded
	case result == "canceled":
		return readmodel.ActivityCanceled
	default:
		return readmodel.ActivityFailed
	}
}

func trafficDuration(row operatorclient.RecentTrafficRow) time.Duration {
	if row.Timing == nil || row.Timing.DurMillis == nil {
		return 0
	}
	return time.Duration(*row.Timing.DurMillis) * time.Millisecond
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
