package adapters

import (
	"context"
	"strings"
	"time"

	operatorclient "github.com/swobuforge/swobu/internal/app/operator/client"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

func (a *LiveOperatorAdapter) ListActivity(ctx context.Context, request ports.ListActivityRequest) (readmodel.ActivityReadModel, error) {
	projection, err := a.client.Activity(ctx, workspaceScope(request.WorkspaceID))
	if err != nil {
		return readmodel.ActivityReadModel{}, adapterFailure("list activity", err)
	}
	return activityFromProjection(projection, request.Limit), nil
}

func workspaceScope(id readmodel.WorkspaceID) string {
	if id == "" {
		return ""
	}
	return string(id)
}

func (a *LiveOperatorAdapter) activityForWorkspace(ctx context.Context, workspaceID readmodel.WorkspaceID) (readmodel.ActivityReadModel, error) {
	return a.ListActivity(ctx, ports.ListActivityRequest{WorkspaceID: workspaceID, Limit: 5})
}

func activityFromProjection(projection operatorclient.ActivityProjection, limit int) readmodel.ActivityReadModel {
	rows := make([]readmodel.ActivityRowReadModel, 0, len(projection.Exchanges))
	for _, exchange := range projection.Exchanges {
		rows = append(rows, activityRowFromExchange(exchange))
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

func activityRowFromExchange(row operatorclient.ActivityExchange) readmodel.ActivityRowReadModel {
	projected := readmodel.ActivityRowReadModel{
		ID:         readmodel.ActivityID(row.ExchangeID),
		ObservedAt: strings.TrimSpace(row.StartedAt), // swobu:io-string source=boundary
		// Evidence owns bounded client-family classification; Cockpit does not
		// infer a family from version-bearing client headers.
		ClientLabel: row.ClientFamily,
		RouteID:     readmodel.RouteID(row.Route), RouteLabel: row.Route,
		ProviderSpec: row.Provider, ProviderModel: row.Model,
		Status: activityStatus(row.State, row.HTTPStatus), HTTPStatus: row.HTTPStatus,
		AttemptCount: row.AttemptCount,
		Error:        row.HTTPStatus >= 400 || row.State == "failed",
		TokensIn:     inputTokens(row.TokenUsage), TokensOut: outputTokens(row.TokenUsage),
	}
	if row.DurationMS != nil {
		projected.Duration = time.Duration(*row.DurationMS) * time.Millisecond
		projected.DurationKnown = true
	}
	return projected
}

func activityStatus(result string, statusCode int) readmodel.ActivityStatus {
	result = strings.ToLower(strings.TrimSpace(result)) // swobu:io-string source=boundary
	switch {
	case result == "in_progress":
		return readmodel.ActivityPending
	case statusCode >= 400:
		return readmodel.ActivityFailed
	case result == "", result == "succeeded":
		return readmodel.ActivitySucceeded
	case result == "canceled", result == "cancelled":
		return readmodel.ActivityCanceled
	default:
		return readmodel.ActivityFailed
	}
}

func inputTokens(usage *operatorclient.ActivityTokenUsage) int {
	if usage != nil && usage.InputTokens != nil {
		return *usage.InputTokens
	}
	return 0
}

func outputTokens(usage *operatorclient.ActivityTokenUsage) int {
	if usage != nil && usage.OutputTokens != nil {
		return *usage.OutputTokens
	}
	return 0
}
