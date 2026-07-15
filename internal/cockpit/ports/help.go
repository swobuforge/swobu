package ports

import "context"

// HelpActions performs support exits from the help tab.
//
// Implementations own browser opening, clipboard access, diagnostics gathering,
// redaction, and fallback file writing. The help surface only displays the
// result state and invokes these actions through this port.
type HelpActions interface {
	OpenDocs(ctx context.Context) error
	OpenCommunity(ctx context.Context) error
	OpenIssue(ctx context.Context) error
	CopyDiagnostics(ctx context.Context) (DiagnosticsCopyResult, error)
}

// DiagnosticsCopyResult reports the visible outcome of copying support context.
type DiagnosticsCopyResult struct {
	Status DiagnosticsCopyStatus
	Path   string
	Text   string
}

// DiagnosticsCopyStatus is the port-level result of a diagnostics copy attempt.
type DiagnosticsCopyStatus int

const (
	DiagnosticsCopyCopied DiagnosticsCopyStatus = iota
	DiagnosticsCopySaved
	DiagnosticsCopyFailed
)
