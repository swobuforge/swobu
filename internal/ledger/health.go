package ledger

type State string

const (
	StateDisabled State = "disabled"
	StateHealthy  State = "healthy"
	StateDegraded State = "degraded"
)

func (s State) Valid() bool { return s == StateDisabled || s == StateHealthy || s == StateDegraded }

type ErrorCode string

const (
	ErrorInvalidConfig        ErrorCode = "invalid_config"
	ErrorOpenFailed           ErrorCode = "open_failed"
	ErrorSchemaNewer          ErrorCode = "schema_newer"
	ErrorSchemaInvalid        ErrorCode = "schema_invalid"
	ErrorMigrationFailed      ErrorCode = "migration_failed"
	ErrorWriterFailed         ErrorCode = "writer_failed"
	ErrorRetentionFailed      ErrorCode = "retention_failed"
	ErrorShutdownTimeout      ErrorCode = "shutdown_timeout"
	ErrorSingleWriterConflict ErrorCode = "single_writer_conflict"
	ErrorHistoryIncomplete    ErrorCode = "history_incomplete"
)

func (c ErrorCode) Valid() bool {
	switch c {
	case ErrorInvalidConfig, ErrorOpenFailed, ErrorSchemaNewer, ErrorSchemaInvalid, ErrorMigrationFailed, ErrorWriterFailed, ErrorRetentionFailed, ErrorShutdownTimeout, ErrorSingleWriterConflict, ErrorHistoryIncomplete:
		return true
	default:
		return false
	}
}
