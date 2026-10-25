package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"

	"github.com/swobuforge/swobu/internal/accounting"
	"github.com/swobuforge/swobu/internal/nodeauthority"
	"github.com/swobuforge/swobu/internal/nodefile"
	platformconfig "github.com/swobuforge/swobu/internal/platform/config"
	"github.com/swobuforge/swobu/internal/relaystate"
)

// runConfig validates a single complete desired-state snapshot and the
// separate runtime journal without starting the daemon, acquiring a write
// lock, touching permissions, or resolving an upstream credential.
func runConfig(ctx context.Context, stdout, stderr io.Writer, args []string) ExitCode {
	if len(args) == 0 || args[0] != "validate" {
		fmt.Fprintln(stderr, "usage: swobu config validate [--config <path>]")
		return ExitDown
	}
	fs := flag.NewFlagSet("config validate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "", "existing Node configuration file")
	readOnly := fs.Bool("config-read-only", false, "validate externally managed Node runtime state alongside its configuration")
	if err := fs.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return ExitHealthy
		}
		return ExitDown
	}
	if rejectUnexpectedPositionalArgs(fs, stderr) {
		return ExitDown
	}
	if err := ctx.Err(); err != nil {
		fmt.Fprintln(stderr, err)
		return ExitDown
	}
	path := platformconfig.ResolveConfigPath(*configPath)
	node, err := nodefile.OpenReadOnly(path)
	if err != nil {
		fmt.Fprintf(stderr, "invalid Node document: %v\n", err)
		return ExitDown
	}
	raw, revision, err := node.Snapshot()
	if err != nil {
		fmt.Fprintf(stderr, "invalid Node snapshot: %v\n", err)
		return ExitDown
	}
	summary, err := nodeauthority.Validate(raw)
	if err != nil {
		fmt.Fprintf(stderr, "invalid Node authority: %v\n", err)
		return ExitDown
	}
	runtimeDir, err := platformconfig.NodeRuntimeRoot(path, *readOnly)
	if err != nil {
		fmt.Fprintf(stderr, "invalid runtime state path: %v\n", err)
		return ExitDown
	}
	if err := platformconfig.ValidateNodeRuntimeOwnership(path, *readOnly, runtimeDir); err != nil {
		fmt.Fprintf(stderr, "invalid runtime ownership: %v\n", err)
		return ExitDown
	}
	if err := accounting.ValidateRuntimeJournal(filepath.Join(runtimeDir, "accounting.json")); err != nil {
		fmt.Fprintf(stderr, "invalid accounting runtime journal: %v\n", err)
		return ExitDown
	}
	if err := relaystate.ValidateRuntimeJournal(filepath.Join(runtimeDir, "relay.json")); err != nil {
		fmt.Fprintf(stderr, "invalid relay runtime journal: %v\n", err)
		return ExitDown
	}
	fmt.Fprintf(stdout, "Valid inline Node authority (%d Workspaces, %d Connections, %d Principals, %d publications, %d budget policies); Node revision %s; Relay transport and execution reservations remain separate.\n", summary.Workspaces, summary.Connections, summary.Principals, summary.Publications, summary.BudgetPolicies, revision)
	return ExitHealthy
}
