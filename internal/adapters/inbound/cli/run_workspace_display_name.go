package cli

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	operatorclient "github.com/swobuforge/swobu/internal/app/operator/client"
	platformconfig "github.com/swobuforge/swobu/internal/platform/config"
	"github.com/swobuforge/swobu/internal/routing"
)

// runWorkspaceDisplayName binds trusted consent to inspected presentation
// metadata and its exact Workspace lifetime/revision. It never renames an
// address, refreshes stale consent, or retries an uncertain write.
func runWorkspaceDisplayName(ctx context.Context, client *http.Client, stdout, stderr io.Writer, args []string, runner Runner) ExitCode {
	fs := flag.NewFlagSet("workspace display-name", flag.ContinueOnError)
	fs.SetOutput(stderr)
	slug := fs.String("workspace", "", "immutable Workspace slug")
	name := fs.String("display-name", "", "exact display name; an explicit empty value clears it")
	addr := fs.String("addr", runner.Addr, "local daemon address")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: swobu workspace display-name --workspace <slug> --display-name <name>")
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
	provided := false
	fs.Visit(func(f *flag.Flag) { provided = provided || f.Name == "display-name" })
	parsed, err := routing.ParseWorkspaceSlug(*slug)
	if err != nil || parsed.String() != *slug || !provided {
		fmt.Fprintln(stderr, "An exact Workspace slug and explicit --display-name are required; an empty value clears the name.")
		return ExitDown
	}
	if !utf8.ValidString(*name) || utf8.RuneCountInString(*name) > 128 || strings.TrimSpace(*name) != *name {
		fmt.Fprintln(stderr, "An exact valid display name is required.")
		return ExitDown
	}
	for _, r := range *name {
		if unicode.IsControl(r) {
			fmt.Fprintln(stderr, "Display name cannot contain control characters.")
			return ExitDown
		}
	}
	interactive := runner.IsInteractive
	if interactive == nil {
		interactive = defaultIsInteractive
	}
	if !interactive() {
		fmt.Fprintln(stderr, "Workspace display-name update requires an interactive local operator; no confirmation bypass is supported.")
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
	before, err := api.GetWorkspace(ctx, *slug)
	if err != nil || before.Slug != *slug || before.ManagementRevision == 0 || before.ManagementETag == "" || strings.ContainsAny(before.ManagementETag, "\"\\, \t\r\n") {
		fmt.Fprintln(stderr, "Cannot inspect exact Workspace revision; no update sent.")
		return ExitDown
	}
	fmt.Fprintf(stderr, "Workspace display-name update\nWorkspace: %s\nBefore: %q\nAfter: %q\nRevision: %q\nOnly presentation metadata changes. Slug, endpoint paths, permissions and credentials remain unchanged.\nType NAME %s to confirm this exact update: ", *slug, before.DisplayName, *name, before.ManagementETag, *slug)
	if runner.Stdin == nil {
		fmt.Fprintln(stderr, "Display-name update cancelled; no apply sent.")
		return ExitDown
	}
	line, err := bufio.NewReader(runner.Stdin).ReadString('\n')
	if err != nil || strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r") != "NAME "+*slug {
		fmt.Fprintln(stderr, "Display-name update cancelled; no apply sent.")
		return ExitDown
	}
	if err := ctx.Err(); err != nil {
		fmt.Fprintln(stderr, err)
		return ExitDown
	}
	updated, err := api.UpdateWorkspaceDisplayName(ctx, *slug, *name, before.ManagementETag)
	if err != nil {
		fmt.Fprintln(stderr, err)
		fmt.Fprintf(stderr, "Update not acknowledged; do not infer rollback or automatically retry. Inspect Workspace %s. Retained intent: display_name=%q reviewed revision=%q. A new attempt requires new inspection and fresh confirmation.\n", *slug, *name, before.ManagementETag)
		return ExitDown
	}
	fmt.Fprintf(stdout, "Workspace %s: display_name=%q revision=%q. Slug, paths, permissions and credentials unchanged.\n", updated.Slug, updated.DisplayName, updated.ManagementETag)
	return ExitHealthy
}
