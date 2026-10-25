package cli

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strings"

	platformbrowser "github.com/swobuforge/swobu/internal/platform/browser"
	platformconfig "github.com/swobuforge/swobu/internal/platform/config"
)

// runOpen attaches to or starts the local Node, then exchanges a native-local
// launch request for a short-lived, single-use browser capability. Neither a
// Workspace credential nor a Node credential is written to a URL or file.
func runOpen(ctx context.Context, client *http.Client, stdout, stderr io.Writer, args []string, runner Runner) ExitCode {
	fs := flag.NewFlagSet("open", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		_, _ = fmt.Fprintln(stderr, "usage: swobu open [--addr <loopback-host:port>] [--config <path>]")
		fs.PrintDefaults()
	}
	addr := fs.String("addr", runner.Addr, "local daemon address")
	configPath := fs.String("config", runner.ConfigPath, "daemon config path when starting")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return ExitHealthy
		}
		return ExitDown
	}
	if rejectUnexpectedPositionalArgs(fs, stderr) {
		return ExitDown
	}
	startup, err := platformconfig.ResolveStartupConfig(*addr)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return ExitDown
	}
	// A Web launch is a normal CLI operation, not a terminal session. Keep the
	// first-run disclosure but never make browser activation wait on a keystroke.
	_ = emitVersionNoticeIfConfigured(stdout)
	if err := ensureTelemetryNoticeBeforeDaemonStart(stdout); err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return ExitDown
	}
	attach := runner.AttachOrStart
	if attach == nil {
		attach = defaultAttachOrStart
	}
	if err := attach(ctx, stdout, stderr, client, startup.Addr, platformconfig.ResolveConfigPath(*configPath)); err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return ExitDown
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, platformconfig.BaseURL(startup.Addr)+"/api/v1/browser-launches", nil)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return ExitDown
	}
	response, err := client.Do(request)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "request Web Operator launch: %v\n", err)
		return ExitDown
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		_, _ = fmt.Fprintf(stderr, "Web Operator launch rejected (HTTP %d)\n", response.StatusCode)
		return ExitDown
	}
	var launch struct {
		LaunchPath string `json:"launch_path"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1024)).Decode(&launch); err != nil {
		_, _ = fmt.Fprintln(stderr, "invalid Web Operator launch response")
		return ExitDown
	}
	// Reject a changed path, origin, query or control characters. The server
	// issues exactly 32 cryptographically random bytes as URL-safe base64.
	token, ok := strings.CutPrefix(launch.LaunchPath, "/open#")
	secret, decodeErr := base64.RawURLEncoding.DecodeString(token)
	if !ok || decodeErr != nil || len(secret) != 32 || base64.RawURLEncoding.EncodeToString(secret) != token {
		_, _ = fmt.Fprintln(stderr, "invalid Web Operator launch path")
		return ExitDown
	}
	open := runner.OpenBrowser
	if open == nil {
		open = platformbrowser.Open
	}
	if err := open(platformconfig.BaseURL(startup.Addr) + launch.LaunchPath); err != nil {
		// Do not echo the one-shot bearer URL into shell logs on opener failure.
		_, _ = fmt.Fprintln(stderr, "could not open the local Web Operator; check the default browser and try swobu open again")
		return ExitDown
	}
	_, _ = fmt.Fprintln(stdout, "Swobu Operator opened at "+platformconfig.BaseURL(startup.Addr)+"/workspaces")
	return ExitHealthy
}
