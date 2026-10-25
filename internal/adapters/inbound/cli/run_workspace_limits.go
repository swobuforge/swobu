package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"time"

	operatorclient "github.com/swobuforge/swobu/internal/app/operator/client"
	platformconfig "github.com/swobuforge/swobu/internal/platform/config"
	"github.com/swobuforge/swobu/internal/routing"
)

func runWorkspaceLimits(ctx context.Context, client *http.Client, stdout, stderr io.Writer, args []string, runner Runner) ExitCode {
	flags := flag.NewFlagSet("workspace limits", flag.ContinueOnError)
	flags.SetOutput(stderr)
	workspace := flags.String("workspace", "", "immutable Workspace slug")
	addr := flags.String("addr", runner.Addr, "local daemon address")
	machine := flags.Bool("json", false, "emit typed status JSON with exact decimal-string totals")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "usage: swobu workspace limits --workspace <slug> [--json] [--addr <host:port>]")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return ExitHealthy
		}
		return ExitDown
	}
	if rejectUnexpectedPositionalArgs(flags, stderr) {
		return ExitDown
	}
	slug, err := routing.ParseWorkspaceSlug(*workspace)
	if err != nil || slug.String() != *workspace {
		fmt.Fprintln(stderr, "An exact Workspace slug is required.")
		return ExitDown
	}
	startup, err := platformconfig.ResolveStartupConfig(*addr)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return ExitDown
	}
	if err := requireCompatibleControlPlane(ctx, client, startup.Addr); err != nil {
		fmt.Fprintln(stderr, err)
		return ExitDown
	}
	status, err := operatorclient.New(client, platformconfig.BaseURL(startup.Addr)).LimitsStatus(ctx, *workspace)
	if err != nil {
		fmt.Fprintln(stderr, "Limits status unavailable:", err)
		return ExitDown
	}
	if status.Workspace != *workspace {
		fmt.Fprintln(stderr, "Limits status returned a different Workspace; no status displayed.")
		return ExitDown
	}
	if *machine {
		if err := json.NewEncoder(stdout).Encode(status); err != nil {
			fmt.Fprintln(stderr, err)
			return ExitDown
		}
		return ExitHealthy
	}
	if _, err := fmt.Fprintf(stdout, "Workspace %s estimated spend (%s cents)\nUTC window: %s to %s (end exclusive)\n", status.Workspace, status.Currency, status.WindowStart.Format(time.RFC3339), status.WindowEnd.Format(time.RFC3339)); err != nil {
		fmt.Fprintln(stderr, err)
		return ExitDown
	}
	if status.Limit == nil {
		_, err = fmt.Fprintln(stdout, "Limit: not configured; no enforced spend threshold.")
	} else {
		_, err = fmt.Fprintf(stdout, "Limit: %d cents per UTC %s; mode=%s\nRemaining estimated capacity: %s cents\n", status.Limit.AmountCents, status.Limit.Window, status.Limit.Mode, remainingCapacity(status.RemainingCents))
	}
	if err == nil {
		_, err = fmt.Fprintf(stdout, "Charged estimate: %s cents\nReserved: %s cents; settled estimate: %s cents; uncertain holds: %s cents\nCoverage: %d unknown-price, %d partial-quantity, %d unquoted operations\nSoft estimates, not provider invoices or guaranteed maximum charges. Unknown-priced work may have unmeasured cost.\n", status.ChargedCents, status.ReservedCents, status.SettledCents, status.UncertainCents, status.UnknownPriceOperations, status.PartialQuantityOperations, status.UnquotedOperations)
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return ExitDown
	}
	return ExitHealthy
}

func remainingCapacity(remaining *string) string {
	if remaining == nil {
		return "unavailable"
	}
	return *remaining
}
