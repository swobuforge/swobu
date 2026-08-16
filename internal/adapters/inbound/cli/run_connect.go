package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	operatorclient "github.com/swobuforge/swobu/internal/app/operator/client"
	workspaceapi "github.com/swobuforge/swobu/internal/app/operator/workspaces"
	"github.com/swobuforge/swobu/internal/clientconnect"
	platformconfig "github.com/swobuforge/swobu/internal/platform/config"
)

type connectOperations interface {
	Plan(clientconnect.ClientID, clientconnect.Target) (clientconnect.Plan, error)
	Apply(clientconnect.Plan) error
}

type connectWorkspaceLister interface {
	ListWorkspaces(context.Context) ([]workspaceapi.WorkspaceSummary, error)
}

func runConnect(ctx context.Context, httpClient *http.Client, stdout, stderr io.Writer, args []string, runner Runner) ExitCode {
	usage := func(out io.Writer, fs *flag.FlagSet) {
		_, _ = fmt.Fprintln(out, "usage: swobu connect <client> [--workspace <name>] [--addr <host:port>] [--replace]")
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
	addr := fs.String("addr", "", fmt.Sprintf("address (env: %s) (default: %s)", platformconfig.EnvAddr, platformconfig.DefaultAddr()))
	replace := fs.Bool("replace", false, "replace existing client configuration")
	if err := fs.Parse(args[1:]); err != nil {
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
		_, _ = fmt.Fprintln(stderr, err.Error())
		return ExitDown
	}
	configPath := platformconfig.ResolveConfigPath(runner.ConfigPath)
	attach := runner.ConnectAttach
	if attach == nil {
		attach = defaultAttachOrStart
	}
	if err := attach(ctx, stdout, stderr, httpClient, startup.Addr, configPath); err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return ExitDown
	}
	lister := runner.ConnectWorkspaces
	if lister == nil {
		lister = operatorclient.New(httpClient, platformconfig.BaseURL(startup.Addr))
	}
	summaries, err := lister.ListWorkspaces(ctx)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return ExitDown
	}
	slug, err := resolveConnectWorkspace(summaries, *workspace)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return ExitDown
	}
	target, err := clientconnect.NewTarget(slug, platformconfig.BaseURL(startup.Addr)+"/c/"+slug)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return ExitDown
	}
	ops := runner.ConnectOperations
	if ops == nil {
		ops = clientconnect.NewService()
	}
	plan, err := ops.Plan(clientID, target)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return ExitDown
	}
	renderConnectPlan(stdout, plan, target)
	if plan.AlreadyConfigured() {
		_, _ = fmt.Fprintln(stdout, "configured")
		return ExitHealthy
	}
	if plan.RequiresReplace() && !*replace {
		_, _ = fmt.Fprintln(stderr, "Existing client configuration would be replaced.\nRun again with --replace.")
		return ExitDown
	}
	if err := ops.Apply(plan); err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return ExitDown
	}
	_, _ = fmt.Fprintln(stdout, "configured")
	return ExitHealthy
}

func resolveConnectWorkspace(summaries []workspaceapi.WorkspaceSummary, explicit string) (string, error) {
	explicit = strings.TrimSpace(explicit)
	slugs := make([]string, 0, len(summaries))
	for _, summary := range summaries {
		slugs = append(slugs, summary.Slug)
	}
	sort.Strings(slugs)
	if explicit != "" {
		if len(slugs) == 0 && explicit == "default" {
			return explicit, nil
		}
		for _, slug := range slugs {
			if slug == explicit {
				return slug, nil
			}
		}
		return "", fmt.Errorf("workspace %q is not configured", explicit)
	}
	switch len(slugs) {
	case 0:
		return "default", nil
	case 1:
		return slugs[0], nil
	default:
		return "", fmt.Errorf("Multiple workspaces are configured: %s.\nChoose one with --workspace.", strings.Join(slugs, ", "))
	}
}

func renderConnectPlan(out io.Writer, plan clientconnect.Plan, target clientconnect.Target) {
	if plan.AlreadyConfigured() {
		_, _ = fmt.Fprintf(out, "%s → %s\n", plan.ClientName, target.WorkspaceSlug())
		return
	}
	_, _ = fmt.Fprintln(out, plan.ClientName)
	for _, change := range plan.Changes {
		if !change.BeforeExists {
			_, _ = fmt.Fprintf(out, "  %-9s → %s\n", change.Field, shortConnectValue(target, change.After))
		} else {
			_, _ = fmt.Fprintf(out, "  %-9s %s\n            → %s\n", change.Field, shortConnectValue(target, change.Before), shortConnectValue(target, change.After))
		}
	}
	_, _ = fmt.Fprintf(out, "  %-9s %s\n\n", "writes", plan.ConfigPath)
}

func shortConnectValue(target clientconnect.Target, value string) string {
	prefix := strings.TrimSuffix(target.WorkspaceURL(), "/c/"+target.WorkspaceSlug())
	if strings.HasPrefix(value, prefix+"/c/") {
		return strings.TrimPrefix(value, prefix)
	}
	return value
}
