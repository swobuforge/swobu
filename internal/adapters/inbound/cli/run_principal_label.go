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
	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	platformconfig "github.com/swobuforge/swobu/internal/platform/config"
)

// Label consent binds the inspected Principal revision, never a later reread.
// An uncertain write requires inspection and a newly reviewed decision, not
// automatic replay with replacement consent. No authority is changed by labels.
func runPrincipalLabel(ctx context.Context, client *http.Client, stdout, stderr io.Writer, args []string, runner Runner) ExitCode {
	fs := flag.NewFlagSet("principal label", flag.ContinueOnError)
	fs.SetOutput(stderr)
	id := fs.String("principal", "", "managed Principal ID")
	label := fs.String("label", "", "new display label")
	addr := fs.String("addr", runner.Addr, "local daemon address")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: swobu principal label --principal <id> --label <label>")
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
	if _, err := accessidentity.RestoreManaged(*id); err != nil {
		fmt.Fprintln(stderr, "A managed Principal ID is required.")
		return ExitDown
	}
	if *label == "" || len(*label) > 128 || !utf8.ValidString(*label) || strings.TrimSpace(*label) != *label {
		fmt.Fprintln(stderr, "An exact valid label is required.")
		return ExitDown
	}
	for _, r := range *label {
		if unicode.IsControl(r) {
			fmt.Fprintln(stderr, "Label cannot contain control characters.")
			return ExitDown
		}
	}
	interactive := runner.IsInteractive
	if interactive == nil {
		interactive = defaultIsInteractive
	}
	if !interactive() {
		fmt.Fprintln(stderr, "Principal label update requires an interactive local operator; no confirmation bypass is supported.")
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
	before, err := api.GetPrincipal(ctx, *id)
	if err != nil || before.ID != *id || before.Revision == "" || strings.ContainsAny(before.Revision, "\"\\, \t\r\n") {
		fmt.Fprintln(stderr, "Cannot inspect exact Principal revision; no update sent.")
		return ExitDown
	}
	fmt.Fprintf(stderr, "Principal label update\nPrincipal: %s\nBefore: %q\nAfter: %q\nRevision: %q\nOnly the label changes. Permissions, credentials and revocation remain unchanged.\nType LABEL %s to confirm this exact update: ", *id, before.Label, *label, before.Revision, *id)
	if runner.Stdin == nil {
		fmt.Fprintln(stderr, "Label update cancelled; no request sent.")
		return ExitDown
	}
	line, err := bufio.NewReader(runner.Stdin).ReadString('\n')
	if err != nil || strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r") != "LABEL "+*id {
		fmt.Fprintln(stderr, "Label update cancelled; no request sent.")
		return ExitDown
	}
	if err := ctx.Err(); err != nil {
		fmt.Fprintln(stderr, err)
		return ExitDown
	}
	updated, err := api.UpdatePrincipalLabel(ctx, *id, *label, before.Revision)
	if err != nil {
		fmt.Fprintln(stderr, err)
		fmt.Fprintf(stderr, "Update not acknowledged; do not infer rollback or automatically retry. Inspect Principal %s. Retained intent: label=%q reviewed revision=%q. A new attempt requires new inspection and fresh confirmation.\n", *id, *label, before.Revision)
		return ExitDown
	}
	fmt.Fprintf(stdout, "Principal %s: label=%q status=%s revision=%q. No permission or credential changed.\n", updated.ID, updated.Label, updated.Status, updated.Revision)
	return ExitHealthy
}
