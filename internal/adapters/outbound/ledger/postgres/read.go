package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
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

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func (db *DB) GetExchange(ctx context.Context, id execution.ExchangeID) (executionevidence.CompletedExchange, bool, error) {
	if !id.Valid() {
		return executionevidence.CompletedExchange{}, false, errors.New("GetExchange requires a valid Exchange identity")
	}
	return getExchange(ctx, db.readers, id)
}

func getExchange(ctx context.Context, q queryer, id execution.ExchangeID) (executionevidence.CompletedExchange, bool, error) {
	var accessRaw, workspaceRaw, familyRaw, protocolRaw, operationRaw, resultRaw string
	var startedNS, settledNS, durationNS int64
	var status sql.NullInt64
	var code sql.NullString
	err := q.QueryRowContext(ctx, `SELECT access_id,workspace_slug,started_at_ns,settled_at_ns,duration_ns,client_family,client_protocol,normalized_operation,delivery_result,client_status_code,canonical_error_code FROM swobu_ledger_exchanges WHERE exchange_id=$1`, id.String()).Scan(&accessRaw, &workspaceRaw, &startedNS, &settledNS, &durationNS, &familyRaw, &protocolRaw, &operationRaw, &resultRaw, &status, &code)
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
	rows, err := q.QueryContext(ctx, `SELECT i.invocation_id,i.kind,i.started_at_ns,i.settled_at_ns,i.duration_ns,i.outcome,i.execution_state,i.failure_class,i.route_name,i.target_id,i.target_version_dec,i.provider_spec,i.provider_protocol,i.provider_model,i.purpose,i.input_tokens,i.output_tokens,i.reasoning_tokens,i.cache_read_tokens,i.cache_write_tokens,i.source_tool_key,i.tool_key FROM swobu_ledger_invocations i WHERE i.exchange_id=$1 ORDER BY i.ordinal`, exchangeID.String())
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
		var sourceToolRaw, toolRaw sql.NullString
		if err := rows.Scan(&idRaw, &kind, &startedNS, &settledNS, &durationNS, &outcomeRaw, &occurrenceRaw, &failure, &routeRaw, &targetRaw, &versionRaw, &providerRaw, &providerProtocolRaw, &modelRaw, &purposeRaw, &input, &output, &reasoning, &cacheRead, &cacheWrite, &sourceToolRaw, &toolRaw); err != nil {
			return nil, err
		}
		id, err := execution.ParseInvocationID(idRaw)
		if err != nil || id.ExchangeID() != exchangeID {
			return nil, errors.New("persisted Invocation identity is invalid")
		}
		outcome, occurrence := execution.Outcome(outcomeRaw), execution.Occurrence(occurrenceRaw)
		row := rowcodec.Invocation{Kind: kind, Route: routeRaw.String, Target: targetRaw.String, Version: versionRaw.String, Provider: providerRaw.String, Protocol: providerProtocolRaw.String, Model: modelRaw.String, Purpose: purposeRaw.String, SourceTool: sourceToolRaw.String, Tool: toolRaw.String, Input: nullIntPtr(input), Output: nullIntPtr(output), Reasoning: nullIntPtr(reasoning), CacheRead: nullIntPtr(cacheRead), CacheWrite: nullIntPtr(cacheWrite)}
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

func restoreUsage(values ...sql.NullInt64) (canonical.TokenUsage, error) {
	p := make([]*int, len(values))
	for i, v := range values {
		if v.Valid {
			n := int(v.Int64)
			p[i] = &n
		}
	}
	return canonical.NewTokenUsage(canonical.TokenUsageParams{InputTokens: p[0], OutputTokens: p[1], ReasoningTokens: p[2], CacheReadTokens: p[3], CacheWriteTokens: p[4]})
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
	rows, err := db.readers.QueryContext(ctx, query, args...)
	if err != nil {
		return ledger.ExchangeSummaryPage{}, err
	}
	type key struct {
		id      execution.ExchangeID
		settled time.Time
		summary ledger.ExchangeSummary
	}
	var keys []key
	for rows.Next() {
		var raw, access, workspace, family, protocol, operation, result string
		var startedNS, ns, durationNS int64
		var status sql.NullInt64
		var code sql.NullString
		if err := rows.Scan(&raw, &access, &workspace, &startedNS, &ns, &durationNS, &family, &protocol, &operation, &result, &status, &code); err != nil {
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
		keys = append(keys, key{id, fromUnixNano(ns), ledger.ExchangeSummary{ID: id, Access: accessID, Workspace: workspaceID, StartedAt: fromUnixNano(startedNS), SettledAt: fromUnixNano(ns), Duration: time.Duration(durationNS), ClientFamily: clientfamily.Family(family), ClientProtocol: protocolID, Operation: canonical.NormalizedPath(operation), Result: delivery.ResultKind(result), Status: int(status.Int64), Error: canonical.ErrorCode(code.String)}})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return ledger.ExchangeSummaryPage{}, err
	}
	more := len(keys) > limit
	if more {
		keys = keys[:limit]
	}
	page := ledger.ExchangeSummaryPage{Exchanges: make([]ledger.ExchangeSummary, 0, len(keys))}
	for _, k := range keys {
		page.Exchanges = append(page.Exchanges, k.summary)
	}
	if more && len(keys) > 0 {
		page.Next, err = ledger.NewCursor(keys[len(keys)-1].settled, keys[len(keys)-1].id)
	}
	return page, err
}

type sqlBuilder struct {
	where []string
	args  []any
}

func (b *sqlBuilder) add(column string, value any) {
	b.args = append(b.args, value)
	b.where = append(b.where, column+" = $"+strconv.Itoa(len(b.args)))
}
func (b *sqlBuilder) addExpr(format string, values ...any) {
	numbers := make([]any, len(values))
	for i, v := range values {
		b.args = append(b.args, v)
		numbers[i] = len(b.args)
	}
	b.where = append(b.where, fmt.Sprintf(format, numbers...))
}

func exchangeListQuery(f ledger.ExchangeFilter, c ledger.Cursor, limit int) (string, []any) {
	b := sqlBuilder{}
	if !f.Settled.Start.IsZero() {
		b.add("e.settled_at_ns", unixNano(f.Settled.Start))
		b.where[len(b.where)-1] = strings.Replace(b.where[len(b.where)-1], " = ", " >= ", 1)
	}
	if !f.Settled.End.IsZero() {
		b.add("e.settled_at_ns", unixNano(f.Settled.End))
		b.where[len(b.where)-1] = strings.Replace(b.where[len(b.where)-1], " = ", " < ", 1)
	}
	if f.Workspace != nil {
		b.add("e.workspace_slug", f.Workspace.String())
	}
	if f.Access != nil {
		b.add("e.access_id", f.Access.String())
	}
	if f.ClientFamily != nil {
		b.add("e.client_family", *f.ClientFamily)
	}
	if f.ClientProtocol != nil {
		b.add("e.client_protocol", *f.ClientProtocol)
	}
	if f.DeliveryResult != nil {
		b.add("e.delivery_result", *f.DeliveryResult)
	}
	if f.CanonicalError != nil {
		b.add("e.canonical_error_code", *f.CanonicalError)
	}
	var inv []string
	addInv := func(col string, v any) {
		b.args = append(b.args, v)
		inv = append(inv, col+" = $"+strconv.Itoa(len(b.args)))
	}
	if f.Route != nil {
		addInv("i.route_name", f.Route.String())
	}
	if f.Provider != nil {
		addInv("i.provider_spec", *f.Provider)
	}
	if f.ProviderModel != nil {
		addInv("i.provider_model", f.ProviderModel.String())
	}
	if f.Target != nil {
		addInv("i.target_id", f.Target.String())
	}
	if f.InvocationKind != nil {
		addInv("i.kind", *f.InvocationKind)
	}
	if f.InvocationOutcome != nil {
		addInv("i.outcome", *f.InvocationOutcome)
	}
	if f.InvocationState != nil {
		addInv("i.execution_state", *f.InvocationState)
	}
	if f.ModelFailure != nil {
		addInv("i.kind", string(ledger.InvocationModel))
		addInv("i.failure_class", *f.ModelFailure)
	}
	if f.MCPFailure != nil {
		addInv("i.kind", string(ledger.InvocationMCP))
		addInv("i.failure_class", *f.MCPFailure)
	}
	if len(inv) > 0 {
		b.where = append(b.where, "EXISTS (SELECT 1 FROM swobu_ledger_invocations i WHERE i.exchange_id=e.exchange_id AND "+strings.Join(inv, " AND ")+")")
	}
	if c.Valid() {
		b.addExpr("(e.settled_at_ns < $%d OR (e.settled_at_ns = $%d AND e.exchange_id < $%d))", unixNano(c.SettledAt()), unixNano(c.SettledAt()), c.ExchangeID().String())
	}
	query := "SELECT e.exchange_id,e.access_id,e.workspace_slug,e.started_at_ns,e.settled_at_ns,e.duration_ns,e.client_family,e.client_protocol,e.normalized_operation,e.delivery_result,e.client_status_code,e.canonical_error_code FROM swobu_ledger_exchanges e"
	if len(b.where) > 0 {
		query += " WHERE " + strings.Join(b.where, " AND ")
	}
	b.args = append(b.args, limit)
	query += " ORDER BY e.settled_at_ns DESC,e.exchange_id DESC LIMIT $" + strconv.Itoa(len(b.args))
	return query, b.args
}

func fromUnixNano(v int64) time.Time { return time.Unix(0, v).UTC() }
