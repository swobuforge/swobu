package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strings"

	operatorclient "github.com/swobuforge/swobu/internal/app/operator/client"
	"github.com/swobuforge/swobu/internal/clientconnect"
	platformconfig "github.com/swobuforge/swobu/internal/platform/config"
	"github.com/swobuforge/swobu/internal/routing"
)

func runConnect(ctx context.Context, httpClient *http.Client, stdout, stderr io.Writer, args []string, runner Runner) ExitCode {
	usage := func(out io.Writer, fs *flag.FlagSet) {
		_, _ = fmt.Fprintln(out, "usage: swobu connect <client> --workspace <slug> --principal <id> --model <model-id> [--addr <host:port>] [--dry-run [--json] | (--apply | --recover) --intent-file <absolute-private-file>]")
		_, _ = fmt.Fprintln(out, "\nclients:")
		for _, id := range clientconnect.AutomaticClientIDs() {
			_, _ = fmt.Fprintf(out, "  %s\n", id)
		}
		if fs != nil {
			_, _ = fmt.Fprintln(out)
			fs.PrintDefaults()
		}
	}
	if len(args) == 0 {
		usage(stderr, nil)
		return ExitDown
	}
	if args[0] == "--help" || args[0] == "-h" {
		usage(stdout, nil)
		return ExitHealthy
	}
	clientID, err := clientconnect.ParseClientID(args[0])
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return ExitDown
	}

	fs := flag.NewFlagSet("connect", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { usage(stderr, fs) }
	workspace := fs.String("workspace", "", "workspace name")
	principal := fs.String("principal", "", "existing Principal with Workspace Models permission")
	modelID := fs.String("model", "", "concrete model ID matched by the Workspace route patterns")
	addr := fs.String("addr", runner.Addr, fmt.Sprintf("address (env: %s) (default: %s)", platformconfig.EnvAddr, platformconfig.DefaultAddr()))
	dryRun := fs.Bool("dry-run", false, "preview through local Node Control without applying or starting a daemon")
	machine := fs.Bool("json", false, "emit typed preview JSON (requires --dry-run)")
	applyReviewed := fs.Bool("apply", false, "apply canonical review after literal local confirmation")
	recoverReviewed := fs.Bool("recover", false, "recover original private intent after fresh local confirmation")
	intentFile := fs.String("intent-file", "", "absolute file in an existing local directory for original reviewed intent")
	if err := fs.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return ExitHealthy
		}
		return ExitDown
	}
	if rejectUnexpectedPositionalArgs(fs, stderr) {
		return ExitDown
	}
	if *modelID == "" {
		fmt.Fprintln(stderr, "--model is required; select a model ID matched by a configured route.")
		return ExitDown
	}
	if *applyReviewed || *recoverReviewed || *intentFile != "" {
		if (*applyReviewed == *recoverReviewed) || *intentFile == "" || *dryRun || *machine {
			fmt.Fprintln(stderr, "Use exactly one of --apply or --recover with --intent-file; do not combine with --dry-run, --json.")
			return ExitDown
		}
		return runReviewedConnect(ctx, httpClient, stdout, stderr, runner, *workspace, string(clientID), *principal, *modelID, *addr, *intentFile, *recoverReviewed)
	}
	if *machine && !*dryRun {
		fmt.Fprintln(stderr, "--json requires --dry-run.")
		return ExitDown
	}
	if *dryRun {
		slug, err := routing.ParseWorkspaceSlug(*workspace)
		if err != nil || slug.String() != *workspace {
			fmt.Fprintln(stderr, "Preview requires an exact --workspace slug.")
			return ExitDown
		}
		startup, err := platformconfig.ResolveStartupConfig(*addr)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return ExitDown
		}
		if err := requireCompatibleControlPlane(ctx, httpClient, startup.Addr); err != nil {
			fmt.Fprintln(stderr, err)
			return ExitDown
		}
		api := operatorclient.New(httpClient, platformconfig.BaseURL(startup.Addr))
		plan, err := api.PreviewClientConfiguration(ctx, *workspace, string(clientID), *principal, *modelID)
		if err != nil {
			fmt.Fprintf(stderr, "Configuration preview unavailable; nothing applied: %v\n", err)
			return ExitDown
		}
		if *machine {
			if err := json.NewEncoder(stdout).Encode(plan); err != nil {
				fmt.Fprintln(stderr, "Preview output unavailable; nothing applied.")
				return ExitDown
			}
			return ExitHealthy
		}
		var output strings.Builder
		fmt.Fprintf(&output, "Preview only: %s in Workspace %s\n", plan.Client.Name, plan.Workspace)
		fmt.Fprintf(&output, "Principal: %s; credential provisioning required: %t\n", plan.Authentication.Principal, plan.Authentication.IssueCredential)
		for _, path := range plan.ConfigPaths {
			fmt.Fprintf(&output, "File: %s\n", path)
		}
		for _, change := range plan.Changes {
			fmt.Fprintf(&output, "%s: %q -> %q (previously present: %t)\n", change.Field, change.Before, change.After, change.BeforeExists)
		}
		fmt.Fprintf(&output, "Already configured: %t; replacement required: %t\nNothing applied. Preview is not approval. Before-values are private operator data.\n", plan.AlreadyConfigured, plan.RequiresReplace)
		if _, err := io.WriteString(stdout, output.String()); err != nil {
			fmt.Fprintln(stderr, "Preview output unavailable; nothing applied.")
			return ExitDown
		}
		return ExitHealthy
	}
	fmt.Fprintln(stderr, "Connect requires --dry-run, or confirmed --apply/--recover with --intent-file. No apply sent.")
	return ExitDown
}
