package ledger

import (
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/domain/execution"
	"github.com/swobuforge/swobu/internal/domain/executionevidence"
)

func TestExchangeFilterRejectsCrossKindFailureRequirements(t *testing.T) {
	modelKind := InvocationModel
	mcpFailure := executionevidence.FailureToolError
	if err := (ExchangeFilter{InvocationKind: &modelKind, MCPFailure: &mcpFailure}).Validate(); err == nil {
		t.Fatal("model Invocation filter accepted an MCP failure class")
	}
	modelFailure := executionevidence.FailureTimeout
	if err := (ExchangeFilter{ModelFailure: &modelFailure, MCPFailure: &mcpFailure}).Validate(); err == nil {
		t.Fatal("filter accepted mutually exclusive model and MCP failure classes")
	}
}

func TestCursorAndPageSizeContract(t *testing.T) {
	mcp, err := NewMCPCursor(time.Unix(1, 2), "mcp_boundary")
	if err != nil || !mcp.Valid() || mcp.OperationID() != "mcp_boundary" || !mcp.SettledAt().Equal(time.Unix(1, 2)) || (MCPCursor{}).Valid() {
		t.Fatalf("MCP cursor=%#v err=%v", mcp, err)
	}
	if _, err := NewMCPCursor(time.Time{}, "mcp_boundary"); err == nil {
		t.Fatal("zero settlement accepted")
	}
	if _, err := NewMCPCursor(time.Now(), ""); err == nil {
		t.Fatal("empty MCP identity accepted")
	}
	id, _ := execution.NewExchangeID()
	cursor, err := NewCursor(time.Unix(1, 2), id)
	if err != nil || !cursor.Valid() || cursor.ExchangeID() != id || cursor.SettledAt() != time.Unix(1, 2) {
		t.Fatalf("cursor=%#v err=%v", cursor, err)
	}
	if got, err := NormalizePageSize(0); err != nil || got != DefaultPageSize {
		t.Fatalf("default page size = %d, %v", got, err)
	}
	if got, err := NormalizePageSize(MaximumPageSize); err != nil || got != MaximumPageSize {
		t.Fatalf("maximum page size = %d, %v", got, err)
	}
	for _, invalid := range []int{-1, MaximumPageSize + 1} {
		if _, err := NormalizePageSize(invalid); err == nil {
			t.Fatalf("invalid page size %d accepted", invalid)
		}
	}
}

func TestRetentionDefaultsAreBoundedAndBackendSpecific(t *testing.T) {
	sqlite := DefaultSQLiteRetention()
	postgres := DefaultPostgresRetention()
	if err := sqlite.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := postgres.Validate(); err != nil {
		t.Fatal(err)
	}
	if sqlite.SQLiteMaxBytes == nil || *sqlite.SQLiteMaxBytes != DefaultSQLiteMaxBytes {
		t.Fatalf("SQLite byte budget = %#v", sqlite.SQLiteMaxBytes)
	}
	if postgres.SQLiteMaxBytes != nil {
		t.Fatal("PostgreSQL default acquired a physical byte budget")
	}
	zero := int64(0)
	if err := (RetentionConfig{DetailDays: 1, SQLiteMaxBytes: &zero}).Validate(); err == nil {
		t.Fatal("zero byte budget accepted")
	}
}

func TestHealthVocabularyIsClosed(t *testing.T) {
	for _, state := range []State{StateDisabled, StateHealthy, StateDegraded} {
		if !state.Valid() {
			t.Fatalf("declared state %q is invalid", state)
		}
	}
	for _, code := range []ErrorCode{
		ErrorInvalidConfig, ErrorOpenFailed, ErrorSchemaNewer, ErrorSchemaInvalid,
		ErrorMigrationFailed, ErrorWriterFailed,
		ErrorRetentionFailed, ErrorShutdownTimeout,
		ErrorSingleWriterConflict,
	} {
		if !code.Valid() {
			t.Fatalf("declared error code %q is invalid", code)
		}
	}
	if State("unknown").Valid() || ErrorCode("backend prose").Valid() {
		t.Fatal("open health vocabulary accepted")
	}
}
