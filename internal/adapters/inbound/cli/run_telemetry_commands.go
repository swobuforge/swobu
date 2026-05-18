package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/swobuforge/swobu/internal/telemetry"
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
	store := telemetry.NewStore()
	state, err := store.LoadOrCreate()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return ExitDown
	}
	payload := struct {
		Enabled            bool   `json:"enabled"`
		DoNotTrack         bool   `json:"do_not_track"`
		AnonymousInstallID string `json:"anonymous_install_id"`
		FirstSeenAt        string `json:"first_seen_at"`
		NoticeShown        bool   `json:"notice_shown"`
		LastUploadAt       string `json:"last_upload_at,omitempty"`
	}{
		Enabled:            state.Enabled && !telemetry.DoNotTrackEnabled(),
		DoNotTrack:         telemetry.DoNotTrackEnabled(),
		AnonymousInstallID: state.AnonymousInstallID,
		FirstSeenAt:        state.FirstSeenAt,
		NoticeShown:        state.NoticeShown,
		LastUploadAt:       state.LastUploadAt,
	}
	if err := json.NewEncoder(stdout).Encode(payload); err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return ExitDown
	}
	return ExitHealthy
}

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
	store := telemetry.NewStore()
	state, err := store.SetEnabled(enabled)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return ExitDown
	}
	payload := struct {
		Enabled bool `json:"enabled"`
	}{
		Enabled: state.Enabled,
	}
	if err := json.NewEncoder(stdout).Encode(payload); err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return ExitDown
	}
	return ExitHealthy
}
