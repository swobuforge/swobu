// Package outputruntime owns vocabulary shared by independent bounded outputs.
package outputruntime

type ErrorCode string

const (
	ErrorQueueOverflow   ErrorCode = "queue_overflow"
	ErrorExportFailed    ErrorCode = "export_failed"
	ErrorShutdownTimeout ErrorCode = "shutdown_timeout"
)
