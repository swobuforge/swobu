package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	operatorclient "github.com/swobuforge/swobu/internal/app/operator/client"
	platformconfig "github.com/swobuforge/swobu/internal/platform/config"
)

// runPrincipalList reads the same Principal projection as the Web Operator.
// Extra flags and positional arguments must never silently select an unintended
// Node. JSON mode retains the typed projection; the table escapes terminal
// control characters in remotely supplied labels.
func runPrincipalList(ctx context.Context, client *http.Client, stdout, stderr io.Writer, args []string, defaultAddr string) ExitCode {
	fs := flag.NewFlagSet("principal list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	addr := fs.String("addr", defaultAddr, fmt.Sprintf("address (env: %s) (default: %s)", platformconfig.EnvAddr, platformconfig.DefaultAddr()))
	machine := fs.Bool("json", false, "emit the typed Principal projection")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: swobu principal list [--addr <loopback-host:port>] [--json]")
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
	startup, err := platformconfig.ResolveStartupConfig(*addr)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return ExitDown
	}
	if err := requireCompatibleControlPlane(ctx, client, startup.Addr); err != nil {
		fmt.Fprintln(stderr, err)
		return ExitDown
	}
	principals, err := operatorclient.New(client, platformconfig.BaseURL(startup.Addr)).ListPrincipals(ctx)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return ExitDown
	}
	if *machine {
		if err := json.NewEncoder(stdout).Encode(principals); err != nil {
			fmt.Fprintln(stderr, "Principal list output unavailable")
			return ExitDown
		}
		return ExitHealthy
	}
	if _, err := fmt.Fprintln(stdout, "ID\tLABEL\tKIND\tSTATUS\tWORKSPACES"); err != nil {
		return ExitDown
	}
	for _, principal := range principals {
		workspaces := make([]string, 0, len(principal.Permissions))
		for _, permission := range principal.Permissions {
			if permission.Action == "use" {
				workspaces = append(workspaces, permission.Workspace)
			}
		}
		_, err := fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\t%s\n", safeTableCell(principal.ID), safeTableCell(principal.Label), safeTableCell(principal.Kind), safeTableCell(principal.Status), safeTableCell(strings.Join(workspaces, ",")))
		if err != nil {
			return ExitDown
		}
	}
	return ExitHealthy
}

func safeTableCell(value string) string {
	// strconv.Quote escapes C0/DEL characters (including ANSI escape, tabs and
	// newlines), so a stored label cannot forge another terminal row. Strip the
	// enclosing quotes to preserve the established table shape.
	quoted := strconv.Quote(value)
	return quoted[1 : len(quoted)-1]
}
