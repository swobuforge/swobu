package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/clientfamily"
	"github.com/swobuforge/swobu/internal/domain/execution"
	"github.com/swobuforge/swobu/internal/domain/executionevidence"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/ledger"
	"github.com/swobuforge/swobu/internal/ledger/rowcodec"
	"github.com/swobuforge/swobu/internal/routing"
)

func (db *DB) GetExchange(ctx context.Context, id execution.ExchangeID) (executionevidence.CompletedExchange, bool, error) {
	if !id.Valid() {
		return executionevidence.CompletedExchange{}, false, errors.New("GetExchange requires a valid Exchange identity")
	}
	return db.getExchange(ctx, db.pool, id)
}

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func (db *DB) getExchange(ctx context.Context, q queryer, id execution.ExchangeID) (executionevidence.CompletedExchange, bool, error) {
	var accessRaw, workspaceRaw, familyRaw, protocolRaw, operationRaw, resultRaw string
	var startedNS, settledNS, durationNS int64
	var status sql.NullInt64
	var code sql.NullString
	err := q.QueryRowContext(ctx, `SELECT access_id, workspace_slug, started_at_ns, settled_at_ns, duration_ns,
client_family, client_protocol, normalized_operation, delivery_result, client_status_code, canonical_error_code
FROM swobu_ledger_exchanges WHERE exchange_id = ?`, id.String()).Scan(&accessRaw, &workspaceRaw, &startedNS, &settledNS, &durationNS, &familyRaw, &protocolRaw, &operationRaw, &resultRaw, &status, &code)
	if errors.Is(err, sql.ErrNoRows) {
		return executionevidence.CompletedExchange{}, false, nil
	}
	if err != nil {
		return executionevidence.CompletedExchange{}, false, err
	}
	access, err := accessidentity.Parse(accessRaw)
	if err != nil {
		return executionevidence.CompletedExchange{}, false, err
	}
	workspace, err := routing.ParseWorkspaceSlug(workspaceRaw)
	if err != nil {
		return executionevidence.CompletedExchange{}, false, err
	}
	protocol, err := protocolkind.ParseProtocolKind(protocolRaw)
	if err != nil {
		return executionevidence.CompletedExchange{}, false, err
	}
	started, err := executionevidence.NewExchangeStarted(id, access, workspace, fromUnixNano(startedNS), clientfamily.Family(familyRaw), protocol, canonical.NormalizedPath(operationRaw))
	if err != nil {
		return executionevidence.CompletedExchange{}, false, err
	}
	settled, err := executionevidence.NewExchangeSettled(id, fromUnixNano(settledNS), delivery.ResultKind(resultRaw), int(status.Int64), canonical.ErrorCode(code.String))
	if err != nil {
		return executionevidence.CompletedExchange{}, false, err
	}
	invocations, err := readInvocations(ctx, q, id)
	if err != nil {
		return executionevidence.CompletedExchange{}, false, err
	}
	completed, err := executionevidence.RestoreCompletedExchange(started, invocations, settled, time.Duration(durationNS))
	return completed, err == nil, err
}

func readInvocations(ctx context.Context, q queryer, exchangeID execution.ExchangeID) ([]executionevidence.InvocationLifecycle, error) {
	rows, err := q.QueryContext(ctx, `SELECT i.invocation_id, i.kind, i.started_at_ns, i.settled_at_ns, i.duration_ns, i.outcome, i.execution_state, i.failure_class,
i.route_name, i.target_id, i.target_version_dec, i.provider_spec, i.provider_protocol, i.provider_model, i.purpose,
i.input_tokens, i.output_tokens, i.reasoning_tokens, i.cache_read_tokens, i.cache_write_tokens,
i.source_tool_key, i.tool_key, i.connection_id, i.connection_revision_dec
FROM swobu_ledger_invocations i
WHERE i.exchange_id = ? ORDER BY i.ordinal`, exchangeID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []executionevidence.InvocationLifecycle
	for rows.Next() {
		var idRaw, kind, outcomeRaw, occurrenceRaw string
		var startedNS, settledNS, durationNS int64
		var failure, routeRaw, targetRaw, versionRaw, providerRaw, providerProtocolRaw, modelRaw, purposeRaw sql.NullString
		var input, output, reasoning, cacheRead, cacheWrite sql.NullInt64
		var sourceToolRaw, toolRaw, connectionIDRaw, connectionRevisionRaw sql.NullString
		if err := rows.Scan(&idRaw, &kind, &startedNS, &settledNS, &durationNS, &outcomeRaw, &occurrenceRaw, &failure,
			&routeRaw, &targetRaw, &versionRaw, &providerRaw, &providerProtocolRaw, &modelRaw, &purposeRaw,
			&input, &output, &reasoning, &cacheRead, &cacheWrite, &sourceToolRaw, &toolRaw, &connectionIDRaw, &connectionRevisionRaw); err != nil {
			return nil, err
		}
		id, err := execution.ParseInvocationID(idRaw)
		if err != nil || id.ExchangeID() != exchangeID {
			return nil, errors.New("persisted Invocation identity is invalid")
		}
		outcome, occurrence := execution.Outcome(outcomeRaw), execution.Occurrence(occurrenceRaw)
		row := rowcodec.Invocation{ConnectionID: connectionIDRaw.String, ConnectionRevision: connectionRevisionRaw.String, Kind: kind, Route: routeRaw.String, Target: targetRaw.String, Version: versionRaw.String, Provider: providerRaw.String, Protocol: providerProtocolRaw.String, Model: modelRaw.String, Purpose: purposeRaw.String, SourceTool: sourceToolRaw.String, Tool: toolRaw.String, Input: nullIntPtr(input), Output: nullIntPtr(output), Reasoning: nullIntPtr(reasoning), CacheRead: nullIntPtr(cacheRead), CacheWrite: nullIntPtr(cacheWrite)}
		completed, err := row.Restore(id, fromUnixNano(startedNS), fromUnixNano(settledNS), time.Duration(durationNS), outcome, occurrence, failure.String)
		if err != nil {
			return nil, err
		}
		out = append(out, completed)
	}
	return out, rows.Err()
}

func nullIntPtr(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	n := v.Int64
	return &n
}

func (db *DB) ListRecentSummaries(ctx context.Context, filter ledger.ExchangeFilter, cursor ledger.Cursor, size int) (ledger.ExchangeSummaryPage, error) {
	if err := filter.Validate(); err != nil {
		return ledger.ExchangeSummaryPage{}, err
	}
	limit, err := ledger.NormalizePageSize(size)
	if err != nil {
		return ledger.ExchangeSummaryPage{}, err
	}
	query, args := exchangeListQuery(filter, cursor, limit+1)
	rows, err := db.pool.QueryContext(ctx, query, args...)
	if err != nil {
		return ledger.ExchangeSummaryPage{}, err
	}
	type key struct {
		id      execution.ExchangeID
		settled time.Time
		summary ledger.ExchangeSummary
	}
	keys := make([]key, 0, limit+1)
	for rows.Next() {
		var raw, access, workspace, family, protocol, operation, result string
		var startedNS, settledNS, durationNS int64
		var status sql.NullInt64
		var code sql.NullString
		if err := rows.Scan(&raw, &access, &workspace, &startedNS, &settledNS, &durationNS, &family, &protocol, &operation, &result, &status, &code); err != nil {
			rows.Close()
			return ledger.ExchangeSummaryPage{}, err
		}
		id, err := execution.ParseExchangeID(raw)
		if err != nil {
			rows.Close()
			return ledger.ExchangeSummaryPage{}, err
		}
		accessID, err := accessidentity.Parse(access)
		if err != nil {
			return ledger.ExchangeSummaryPage{}, err
		}
		workspaceID, err := routing.ParseWorkspaceSlug(workspace)
		if err != nil {
			return ledger.ExchangeSummaryPage{}, err
		}
		protocolID, err := protocolkind.ParseProtocolKind(protocol)
		if err != nil {
			return ledger.ExchangeSummaryPage{}, err
		}
		keys = append(keys, key{id, fromUnixNano(settledNS), ledger.ExchangeSummary{ID: id, Access: accessID, Workspace: workspaceID, StartedAt: fromUnixNano(startedNS), SettledAt: fromUnixNano(settledNS), Duration: time.Duration(durationNS), ClientFamily: clientfamily.Family(family), ClientProtocol: protocolID, Operation: canonical.NormalizedPath(operation), Result: delivery.ResultKind(result), Status: int(status.Int64), Error: canonical.ErrorCode(code.String)}})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return ledger.ExchangeSummaryPage{}, err
	}
	hasMore := len(keys) > limit
	if hasMore {
		keys = keys[:limit]
	}
	page := ledger.ExchangeSummaryPage{Exchanges: make([]ledger.ExchangeSummary, 0, len(keys))}
	for _, item := range keys {
		page.Exchanges = append(page.Exchanges, item.summary)
	}
	if hasMore && len(keys) > 0 {
		page.Next, err = ledger.NewCursor(keys[len(keys)-1].settled, keys[len(keys)-1].id)
	}
	return page, err
}

func exchangeListQuery(filter ledger.ExchangeFilter, cursor ledger.Cursor, limit int) (string, []any) {
	var where []string
	var args []any
	if !filter.Settled.Start.IsZero() {
		where = append(where, "e.settled_at_ns >= ?")
		args = append(args, unixNano(filter.Settled.Start))
	}
	if !filter.Settled.End.IsZero() {
		where = append(where, "e.settled_at_ns < ?")
		args = append(args, unixNano(filter.Settled.End))
	}
	add := func(column string, value any) { where = append(where, column+" = ?"); args = append(args, value) }
	if filter.Workspace != nil {
		add("e.workspace_slug", filter.Workspace.String())
	}
	if filter.Access != nil {
		add("e.access_id", filter.Access.String())
	}
	if filter.ClientFamily != nil {
		add("e.client_family", *filter.ClientFamily)
	}
	if filter.ClientProtocol != nil {
		add("e.client_protocol", *filter.ClientProtocol)
	}
	if filter.DeliveryResult != nil {
		add("e.delivery_result", *filter.DeliveryResult)
	}
	if filter.CanonicalError != nil {
		add("e.canonical_error_code", *filter.CanonicalError)
	}
	var invocation []string
	addInvocation := func(column string, value any) {
		invocation = append(invocation, column+" = ?")
		args = append(args, value)
	}
	if filter.Route != nil {
		addInvocation("i.route_name", filter.Route.String())
	}
	if filter.Provider != nil {
		addInvocation("i.provider_spec", *filter.Provider)
	}
	if filter.ProviderModel != nil {
		addInvocation("i.provider_model", filter.ProviderModel.String())
	}
	if filter.Target != nil {
		addInvocation("i.target_id", filter.Target.String())
	}
	if filter.InvocationKind != nil {
		addInvocation("i.kind", *filter.InvocationKind)
	}
	if filter.InvocationOutcome != nil {
		addInvocation("i.outcome", *filter.InvocationOutcome)
	}
	if filter.InvocationState != nil {
		addInvocation("i.execution_state", *filter.InvocationState)
	}
	if filter.ModelFailure != nil {
		addInvocation("i.kind", string(ledger.InvocationModel))
		addInvocation("i.failure_class", *filter.ModelFailure)
	}
	if filter.MCPFailure != nil {
		addInvocation("i.kind", string(ledger.InvocationMCP))
		addInvocation("i.failure_class", *filter.MCPFailure)
	}
	if len(invocation) > 0 {
		where = append(where, "EXISTS (SELECT 1 FROM swobu_ledger_invocations i WHERE i.exchange_id=e.exchange_id AND "+strings.Join(invocation, " AND ")+")")
	}
	if cursor.Valid() {
		where = append(where, "(e.settled_at_ns < ? OR (e.settled_at_ns = ? AND e.exchange_id < ?))")
		args = append(args, unixNano(cursor.SettledAt()), unixNano(cursor.SettledAt()), cursor.ExchangeID().String())
	}
	query := "SELECT e.exchange_id, e.access_id, e.workspace_slug, e.started_at_ns, e.settled_at_ns, e.duration_ns, e.client_family, e.client_protocol, e.normalized_operation, e.delivery_result, e.client_status_code, e.canonical_error_code FROM swobu_ledger_exchanges e"
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY e.settled_at_ns DESC, e.exchange_id DESC LIMIT ?"
	args = append(args, limit)
	return query, args
}

func fromUnixNano(value int64) time.Time { return time.Unix(0, value).UTC() }
