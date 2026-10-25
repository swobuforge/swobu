package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"

	"github.com/swobuforge/swobu/internal/domain/executionevidence"
)

func (db *DB) WriteBatch(ctx context.Context, exchanges []executionevidence.CompletedExchange) error {
	if err := db.writerLockHeld(ctx); err != nil {
		if reconnectErr := db.reconnectWriter(ctx); reconnectErr != nil {
			return err
		}
		if err := db.writerLockHeld(ctx); err != nil {
			return err
		}
	}
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
	s, e := exchange.Started(), exchange.Settled()
	result, err := tx.ExecContext(ctx, `INSERT INTO swobu_ledger_exchanges(exchange_id,access_id,workspace_slug,started_at_ns,settled_at_ns,duration_ns,client_family,client_protocol,normalized_operation,delivery_result,client_status_code,canonical_error_code)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) ON CONFLICT(exchange_id) DO NOTHING`, s.ID().String(), s.AccessID().String(), s.Workspace().String(), unixNano(s.StartedAt()), unixNano(e.SettledAt()), exchange.Duration().Nanoseconds(), s.ClientFamily(), s.ClientProtocol(), s.Operation(), e.Result(), nullableInt(e.Status()), nullableString(string(e.ErrorCode())))
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if rows == 1 {
		return true, nil
	}
	return false, nil
}

func insertInvocations(ctx context.Context, tx *sql.Tx, exchange executionevidence.CompletedExchange) error {
	for _, inv := range exchange.Invocations() {
		start, settled := inv.Started(), inv.Settled()
		kind, failure := "", ""
		switch terminal := settled.(type) {
		case executionevidence.ModelInvocationSettled:
			kind, failure = "model", string(terminal.Failure())
		case executionevidence.MCPInvocationSettled:
			kind, failure = "mcp_tool", string(terminal.Failure())
		default:
			return fmt.Errorf("unsupported Invocation %T", settled)
		}
		values := []any{start.ID().String(), exchange.ID().String(), start.Ordinal(), kind, unixNano(start.StartedAt()), unixNano(settled.SettledAt()), inv.Duration().Nanoseconds(), settled.Outcome(), settled.Occurrence(), nullableString(failure), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil}
		values = append(values, nil, nil)
		switch typed := start.(type) {
		case executionevidence.ModelInvocationStarted:
			if id, revision, bound := typed.Model().ConnectionIdentity(); bound {
				values[24], values[25] = id.String(), strconv.FormatUint(revision, 10)
			}
			terminal := settled.(executionevidence.ModelInvocationSettled)
			usage := terminal.Usage()
			values[10] = nullableString(typed.Model().Route().String())
			values[11] = typed.Model().Target().String()
			values[12] = strconv.FormatUint(uint64(typed.Model().Version()), 10)
			values[13] = typed.Model().Provider()
			values[14] = typed.Model().Protocol()
			values[15] = typed.Model().Model().String()
			values[16] = typed.Model().Purpose()
			values[17] = tokenValue(usage.InputTokens())
			values[18] = tokenValue(usage.OutputTokens())
			values[19] = tokenValue(usage.ReasoningTokens())
			values[20] = tokenValue(usage.CacheReadTokens())
			values[21] = tokenValue(usage.CacheWriteTokens())
		case executionevidence.MCPInvocationStarted:
			if id, revision, bound := typed.MCP().ConnectionIdentity(); bound {
				values[24], values[25] = id.String(), strconv.FormatUint(revision, 10)
			}
			values[22] = typed.MCP().Source().String()
			values[23] = typed.MCP().Tool().String()
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO swobu_ledger_invocations(invocation_id,exchange_id,ordinal,kind,started_at_ns,settled_at_ns,duration_ns,outcome,execution_state,failure_class,route_name,target_id,target_version_dec,provider_spec,provider_protocol,provider_model,purpose,input_tokens,output_tokens,reasoning_tokens,cache_read_tokens,cache_write_tokens,source_tool_key,tool_key,connection_id,connection_revision_dec) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26)`, values...); err != nil {
			return err
		}
	}
	return nil
}

func nullableString(v string) any {
	if v == "" {
		return nil
	}
	return v
}
func nullableInt(v int) any {
	if v == 0 {
		return nil
	}
	return v
}
func tokenValue(v int, k bool) any {
	if !k {
		return nil
	}
	return v
}
func unixNano(t time.Time) int64 { return t.UTC().UnixNano() }
