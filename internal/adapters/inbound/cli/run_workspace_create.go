package cli

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strings"

	operatorclient "github.com/swobuforge/swobu/internal/app/operator/client"
	workspaceapi "github.com/swobuforge/swobu/internal/app/operator/workspaces"
	platformconfig "github.com/swobuforge/swobu/internal/platform/config"
)

// runWorkspaceCreate creates an empty Workspace after server-owned preview and
// literal consent. Recovery retains original slug/name/key, inspects current
// state only as advisory evidence, and requires fresh confirmation before replay.
func runWorkspaceCreate(ctx context.Context, client *http.Client, stdout, stderr io.Writer, args []string, runner Runner) ExitCode {
	fs := flag.NewFlagSet("workspace create", flag.ContinueOnError)
	fs.SetOutput(stderr)
	slug := fs.String("workspace", "", "immutable Workspace slug")
	name := fs.String("display-name", "", "optional display name")
	key := fs.String("retry-key", "", "original creation key for explicit recovery")
	addr := fs.String("addr", runner.Addr, "local daemon address")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: swobu workspace create --workspace <slug> [--display-name <name>] [--retry-key <original-key>]")
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
	recovery := false
	nameProvided := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "retry-key" {
			recovery = true
		}
		if f.Name == "display-name" {
			nameProvided = true
		}
	})
	if recovery && (*key == "" || !nameProvided) {
		fmt.Fprintln(stderr, "Recovery requires original --retry-key and explicit original --display-name (empty is valid).")
		return ExitDown
	}
	interactive := runner.IsInteractive
	if interactive == nil {
		interactive = defaultIsInteractive
	}
	if !interactive() {
		fmt.Fprintln(stderr, "Workspace creation requires an interactive local operator; no confirmation bypass is supported.")
		return ExitDown
	}
	if !recovery {
		var random [32]byte
		if _, err := rand.Read(random[:]); err != nil {
			fmt.Fprintln(stderr, "Cannot issue creation retry key.")
			return ExitDown
		}
		*key = hex.EncodeToString(random[:])
	}
	intent, err := operatorclient.NewWorkspaceCreationIntent(workspaceapi.CreateWorkspace{Slug: *slug, DisplayName: *name}, *key)
	if err != nil {
		fmt.Fprintln(stderr, err)
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
	api := operatorclient.New(client, platformconfig.BaseURL(startup.Addr))
	if !recovery {
		preview, err := api.PreviewWorkspaceCreation(ctx, intent)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return ExitDown
		}
		fmt.Fprintf(stderr, "Workspace creation preview\nWorkspace: %s\nDisplay name: %q\nRoutes: %d; targets: %d\nReadiness: LLM %s; MCP %s\n%s\n%s\n", preview.Slug, preview.DisplayName, preview.RouteCount, preview.TargetCount, preview.Readiness.LLM, preview.Readiness.MCP, preview.Diff, preview.Warning)
	} else {
		current, err := api.GetWorkspace(ctx, *slug)
		if err != nil && !operatorclient.IsNotFound(err) {
			fmt.Fprintln(stderr, "Cannot inspect current Workspace; no recovery sent.")
			return ExitDown
		}
		if err == nil {
			fmt.Fprintf(stderr, "Current advisory state: display_name=%q revision=%q. This is not original consent.\n", current.DisplayName, current.ManagementETag)
		} else {
			fmt.Fprintln(stderr, "Current advisory state: Workspace absent. Recovery can create if no retained receipt exists.")
		}
		fmt.Fprintln(stderr, "Recover original creation only; matching replay returns original metadata, not current state. No new route, permission or credential is requested.")
	}
	fmt.Fprintf(stderr, "Original intent: workspace=%s display_name=%q\nRetry key: %s\nType CREATE %s to confirm this exact creation: ", *slug, *name, *key, *slug)
	if runner.Stdin == nil {
		fmt.Fprintln(stderr, "Creation cancelled; no apply sent.")
		return ExitDown
	}
	line, err := bufio.NewReader(runner.Stdin).ReadString('\n')
	if err != nil || strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r") != "CREATE "+*slug {
		fmt.Fprintln(stderr, "Creation cancelled; no apply sent.")
		return ExitDown
	}
	if err := ctx.Err(); err != nil {
		fmt.Fprintln(stderr, err)
		return ExitDown
	}
	created, replayed, err := api.ApplyWorkspaceCreation(ctx, intent)
	if err != nil {
		fmt.Fprintln(stderr, err)
		fmt.Fprintf(stderr, "Creation not acknowledged; do not infer rollback or automatically retry. Retain original intent: swobu workspace create --workspace %s --display-name %q --retry-key %s. Recovery requires fresh confirmation.\n", *slug, *name, *key)
		return ExitDown
	}
	fmt.Fprintf(stdout, "Workspace %s: original display_name=%q revision=%q replayed=%t. No initial routes, permissions or credentials created.\n", created.Slug, created.DisplayName, created.ManagementETag, replayed)
	return ExitHealthy
}
