package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"

	"github.com/swobuforge/swobu/internal/domain/executionevidence"
)

// WriteBatch atomically commits first-seen completed Exchanges, their detail
// rows and known gap markers.
func (db *DB) WriteBatch(ctx context.Context, exchanges []executionevidence.CompletedExchange) error {
	tx, err := db.writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, exchange := range exchanges {
		inserted, err := insertExchange(ctx, tx, exchange)
		if err != nil {
			return err
		}
		if !inserted {
			continue
		}
		if err := insertInvocations(ctx, tx, exchange); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func insertExchange(ctx context.Context, tx *sql.Tx, exchange executionevidence.CompletedExchange) (bool, error) {
	started, settled := exchange.Started(), exchange.Settled()
	result, err := tx.ExecContext(ctx, `INSERT INTO swobu_ledger_exchanges(
exchange_id, access_id, workspace_slug, started_at_ns, settled_at_ns, duration_ns,
client_family, client_protocol, normalized_operation, delivery_result, client_status_code, canonical_error_code
) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(exchange_id) DO NOTHING`,
		started.ID().String(), started.AccessID().String(), started.Workspace().String(), unixNano(started.StartedAt()), unixNano(settled.SettledAt()), exchange.Duration().Nanoseconds(),
		started.ClientFamily(), started.ClientProtocol(), started.Operation(), settled.Result(), nullableInt(settled.Status()), nullableString(string(settled.ErrorCode())))
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	if err != nil || rows == 1 {
		return rows == 1, err
	}
	return false, nil
}

func insertInvocations(ctx context.Context, tx *sql.Tx, exchange executionevidence.CompletedExchange) error {
	for _, invocation := range exchange.Invocations() {
		started, settled := invocation.Started(), invocation.Settled()
		kind, failure := "", ""
		switch terminal := settled.(type) {
		case executionevidence.ModelInvocationSettled:
			kind, failure = "model", string(terminal.Failure())
		case executionevidence.MCPInvocationSettled:
			kind, failure = "mcp_tool", string(terminal.Failure())
		default:
			return fmt.Errorf("unsupported completed Invocation settlement %T", settled)
		}
		values := []any{started.ID().String(), exchange.ID().String(), started.Ordinal(), kind, unixNano(started.StartedAt()), unixNano(settled.SettledAt()), invocation.Duration().Nanoseconds(), settled.Outcome(), settled.Occurrence(), nullableString(failure), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil}
		values = append(values, nil, nil)
		switch start := started.(type) {
		case executionevidence.ModelInvocationStarted:
			if id, revision, bound := start.Model().ConnectionIdentity(); bound {
				values[24], values[25] = id.String(), strconv.FormatUint(revision, 10)
			}
			terminal := settled.(executionevidence.ModelInvocationSettled)
			usage := terminal.Usage()
			values[10] = nullableString(start.Model().Route().String())
			values[11] = start.Model().Target().String()
			values[12] = strconv.FormatUint(uint64(start.Model().Version()), 10)
			values[13] = start.Model().Provider()
			values[14] = start.Model().Protocol()
			values[15] = start.Model().Model().String()
			values[16] = start.Model().Purpose()
			values[17] = tokenValue(usage.InputTokens())
			values[18] = tokenValue(usage.OutputTokens())
			values[19] = tokenValue(usage.ReasoningTokens())
			values[20] = tokenValue(usage.CacheReadTokens())
			values[21] = tokenValue(usage.CacheWriteTokens())
		case executionevidence.MCPInvocationStarted:
			if id, revision, bound := start.MCP().ConnectionIdentity(); bound {
				values[24], values[25] = id.String(), strconv.FormatUint(revision, 10)
			}
			values[22] = start.MCP().Source().String()
			values[23] = start.MCP().Tool().String()
		default:
			return fmt.Errorf("unsupported completed Invocation start %T", started)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO swobu_ledger_invocations(invocation_id,exchange_id,ordinal,kind,started_at_ns,settled_at_ns,duration_ns,outcome,execution_state,failure_class,route_name,target_id,target_version_dec,provider_spec,provider_protocol,provider_model,purpose,input_tokens,output_tokens,reasoning_tokens,cache_read_tokens,cache_write_tokens,source_tool_key,tool_key,connection_id,connection_revision_dec) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, values...); err != nil {
			return err
		}
	}
	return nil
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
func nullableInt(value int) any {
	if value == 0 {
		return nil
	}
	return value
}
func tokenValue(value int, known bool) any {
	if !known {
		return nil
	}
	return value
}
