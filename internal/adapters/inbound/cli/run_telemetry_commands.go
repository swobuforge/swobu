package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/swobuforge/swobu/internal/producttelemetry"
)

func runTelemetry(stdout io.Writer, stderr io.Writer, args []string) ExitCode {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, "telemetry subcommand required: status|on|off")
		return ExitDown
	}

	telemetrySubcommand := args[0] // swobu:io-string source=cli-args
	switch telemetrySubcommand {
	case "status":
		return runTelemetryStatus(stdout, stderr, args[1:])
	case "on":
		return runTelemetrySetEnabled(stdout, stderr, true, args[1:])
	case "off":
		return runTelemetrySetEnabled(stdout, stderr, false, args[1:])
	default:
		_, _ = fmt.Fprintf(stderr, "unknown telemetry subcommand %q\n", telemetrySubcommand)
		return ExitDown
	}
}

func runTelemetryStatus(stdout io.Writer, stderr io.Writer, args []string) ExitCode {
	fs := flag.NewFlagSet("telemetry status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		_, _ = fmt.Fprintln(stderr, "usage: swobu telemetry status")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return ExitHealthy
		}
		return ExitDown
	}
	if rejectUnexpectedPositionalArgs(fs, stderr) {
		return ExitDown
	}
	status, err := producttelemetry.Status()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return ExitDown
	}
	payload := struct {
		Enabled    bool `json:"enabled"`
		DoNotTrack bool `json:"do_not_track"`
	}{
		Enabled:    status.Enabled,
		DoNotTrack: status.DoNotTrack,
	}
	if err := json.NewEncoder(stdout).Encode(payload); err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return ExitDown
	}
	return ExitHealthy
}

// runTelemetrySetEnabled writes the preference directly to the local preference
// document with a fresh revision. A running daemon's telemetry runtime adopts it
// at its next preference poll and discards any aggregate collected under the old
// revision (product-telemetry.md §6). No daemon control surface is involved.
func runTelemetrySetEnabled(stdout io.Writer, stderr io.Writer, enabled bool, args []string) ExitCode {
	fs := flag.NewFlagSet("telemetry toggle", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		_, _ = fmt.Fprintln(stderr, "usage: swobu telemetry [on|off]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return ExitHealthy
		}
		return ExitDown
	}
	if rejectUnexpectedPositionalArgs(fs, stderr) {
		return ExitDown
	}
	persisted, err := producttelemetry.SetEnabled(enabled)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return ExitDown
	}
	payload := struct {
		Enabled bool `json:"enabled"`
	}{
		Enabled: persisted,
	}
	if err := json.NewEncoder(stdout).Encode(payload); err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return ExitDown
	}
	return ExitHealthy
}
