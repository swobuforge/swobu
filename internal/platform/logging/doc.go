// Package logging configures Swobu's process-wide slog logger.
//
// It owns handler selection at the daemon boundary, not log presentation:
// upstream slog handlers format records. BufferedHandler is retained solely to
// keep asynchronous daemon records from corrupting an interactive Cockpit.
package logging
