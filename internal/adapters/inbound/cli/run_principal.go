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
	"unicode"
	"unicode/utf8"

	operatorclient "github.com/swobuforge/swobu/internal/app/operator/client"
	platformconfig "github.com/swobuforge/swobu/internal/platform/config"
)

// runPrincipal creates attribution only. Exact label/key and fresh consent are
// retained on recovery; neither metadata replay nor creation grants access.
func runPrincipal(ctx context.Context, client *http.Client, stdout, stderr io.Writer, args []string, runner Runner) ExitCode {
	if len(args) > 0 && args[0] == "list" {
		return runPrincipalList(ctx, client, stdout, stderr, args[1:], runner.Addr)
	}
	if len(args) > 0 && args[0] == "revoke" {
		return runPrincipalRevoke(ctx, client, stdout, stderr, args[1:], runner)
	}
	if len(args) > 0 && args[0] == "label" {
		return runPrincipalLabel(ctx, client, stdout, stderr, args[1:], runner)
	}
	if len(args) == 0 || args[0] != "create" {
		fmt.Fprintln(stderr, "usage: swobu principal list|create|label|revoke [args]")
		return ExitDown
	}
	fs := flag.NewFlagSet("principal create", flag.ContinueOnError)
	fs.SetOutput(stderr)
	label := fs.String("label", "", "identity label, not verified human identity")
	retry := fs.String("retry-key", "", "original creation key for explicit recovery")
	addr := fs.String("addr", runner.Addr, "local daemon address")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: swobu principal create --label <label> [--retry-key <original-key>]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return ExitHealthy
		}
		return ExitDown
	}
	if rejectUnexpectedPositionalArgs(fs, stderr) || *label == "" || len(*label) > 128 || !utf8.ValidString(*label) || strings.TrimSpace(*label) != *label {
		fmt.Fprintln(stderr, "An exact nonempty label is required.")
		return ExitDown
	}
	for _, r := range *label {
		if unicode.IsControl(r) {
			fmt.Fprintln(stderr, "Label cannot contain control characters.")
			return ExitDown
		}
	}
	key := *retry
	if key == "" {
		var random [32]byte
		if _, err := rand.Read(random[:]); err != nil {
			fmt.Fprintln(stderr, "Unable to generate creation key.")
			return ExitDown
		}
		key = hex.EncodeToString(random[:])
	}
	if len(key) > 256 {
		fmt.Fprintln(stderr, "Invalid original creation key.")
		return ExitDown
	}
	for _, r := range key {
		if r < 33 || r > 126 {
			fmt.Fprintln(stderr, "Invalid original creation key.")
			return ExitDown
		}
	}
	interactive := runner.IsInteractive
	if interactive == nil {
		interactive = defaultIsInteractive
	}
	if !interactive() {
		fmt.Fprintln(stderr, "Principal creation requires an interactive local operator; no confirmation bypass is supported.")
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
	fmt.Fprintf(stderr, "Principal creation\nLabel: %q\nRetry key: %s\nCreates identity metadata only. No Workspace permission or credential is issued.\n", *label, key)
	if *retry != "" {
		fmt.Fprintln(stderr, "Recovering original intent: if committed, returns current identity metadata; otherwise may create the first identity. Do not change the original label or key.")
	}
	fmt.Fprintf(stderr, "Type CREATE %s to confirm this exact creation: ", *label)
	if runner.Stdin == nil {
		fmt.Fprintln(stderr, "Creation cancelled; no request sent.")
		return ExitDown
	}
	line, err := bufio.NewReader(runner.Stdin).ReadString('\n')
	if err != nil || strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r") != "CREATE "+*label {
		fmt.Fprintln(stderr, "Creation cancelled; no request sent.")
		return ExitDown
	}
	if err := ctx.Err(); err != nil {
		fmt.Fprintln(stderr, err)
		return ExitDown
	}
	principal, replayed, err := operatorclient.New(client, platformconfig.BaseURL(startup.Addr)).CreatePrincipal(ctx, *label, key)
	if err != nil {
		fmt.Fprintln(stderr, err)
		fmt.Fprintf(stderr, "Creation was not acknowledged; do not infer rollback or automatically retry. Retain label %q and retry key %s; recover with swobu principal create --label %q --retry-key %s after fresh confirmation.\n", *label, key, *label, key)
		return ExitDown
	}
	fmt.Fprintf(stdout, "Principal %s: label=%q status=%s replayed=%t. No permission or credential was issued by this request.\n", principal.ID, principal.Label, principal.Status, replayed)
	return ExitHealthy
}
