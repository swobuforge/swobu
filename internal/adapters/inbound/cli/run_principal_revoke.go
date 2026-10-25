package cli

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strings"

	operatorclient "github.com/swobuforge/swobu/internal/app/operator/client"
	platformconfig "github.com/swobuforge/swobu/internal/platform/config"
)

// runPrincipalRevoke binds literal operator consent to server impact or retained
// uncertain intent. Advisory recovery reads never replace the original revision.
func runPrincipalRevoke(ctx context.Context, client *http.Client, stdout, stderr io.Writer, args []string, runner Runner) ExitCode {
	fs := flag.NewFlagSet("principal revoke", flag.ContinueOnError)
	fs.SetOutput(stderr)
	id := fs.String("principal", "", "managed Principal to revoke identity-wide")
	key := fs.String("retry-key", "", "original key for separately confirmed recovery")
	revision := fs.String("expected-revision", "", "original unquoted revision; required with --retry-key")
	addr := fs.String("addr", runner.Addr, "local daemon address")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: swobu principal revoke --principal <id> [--retry-key <key> --expected-revision <revision>]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return ExitHealthy
		}
		return ExitDown
	}
	if rejectUnexpectedPositionalArgs(fs, stderr) || (*key == "") != (*revision == "") {
		fs.Usage()
		return ExitDown
	}
	recovery := *key != ""
	if _, err := operatorclient.RestorePrincipalRevocationIntent(*id, "validation-only", "validation-only"); err != nil {
		fmt.Fprintln(stderr, err)
		return ExitDown
	}
	if recovery {
		if _, err := operatorclient.RestorePrincipalRevocationIntent(*id, *revision, *key); err != nil {
			fmt.Fprintln(stderr, err)
			return ExitDown
		}
	}
	interactive := runner.IsInteractive
	if interactive == nil {
		interactive = defaultIsInteractive
	}
	if !interactive() {
		fmt.Fprintln(stderr, "Principal revocation requires an interactive local operator; no confirmation bypass is supported.")
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
	current, err := api.GetPrincipal(ctx, *id)
	if err != nil {
		fmt.Fprintln(stderr, "Cannot inspect Principal; no revocation sent.")
		return ExitDown
	}
	if recovery {
		metadata, _ := json.Marshal(current)
		fmt.Fprintf(stderr, "Recovery: original revocation may already have committed or may commit for the first time. Current state is advisory: %s\n", metadata)
	} else {
		preview, err := api.PreviewPrincipalRevocation(ctx, *id, current.Revision)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return ExitDown
		}
		var randomKey [32]byte
		if _, err := rand.Read(randomKey[:]); err != nil {
			fmt.Fprintln(stderr, "Unable to create retry key.")
			return ExitDown
		}
		*key, *revision = hex.EncodeToString(randomKey[:]), preview.Principal.Revision
		permissions, _ := json.Marshal(preview.Principal.Permissions)
		fmt.Fprintf(stderr, "Identity-wide revocation\nPrincipal: %q\nLabel: %q\nStatus: %q\nPermissions removed: %s\nCredentials revoked: %d\nIdentity remains for attribution.\n", *id, preview.Principal.Label, preview.Principal.Status, permissions, preview.CredentialCount)
	}
	intent, err := operatorclient.RestorePrincipalRevocationIntent(*id, *revision, *key)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return ExitDown
	}
	fmt.Fprintf(stderr, "Original revision: %q\nRetry key: %s\nType REVOKE %s to confirm this exact identity-wide revocation: ", *revision, *key, *id)
	if runner.Stdin == nil {
		fmt.Fprintln(stderr, "Revocation cancelled; no request sent.")
		return ExitDown
	}
	line, err := bufio.NewReader(runner.Stdin).ReadString('\n')
	if err != nil || strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r") != "REVOKE "+*id || ctx.Err() != nil {
		fmt.Fprintln(stderr, "Revocation cancelled; no request sent.")
		return ExitDown
	}
	result, err := api.ApplyPrincipalRevocation(ctx, intent)
	if err != nil {
		fmt.Fprintln(stderr, err)
		fmt.Fprintf(stderr, "Do not infer rollback or automatically retry. Retain original intent: swobu principal revoke --principal %s --retry-key %s --expected-revision %s\n", *id, *key, *revision)
		return ExitDown
	}
	fmt.Fprintf(stdout, "Principal %s: status=%s replayed=%t revision=%q. Identity retained; no permissions or credentials remain.\n", result.Principal.ID, result.Principal.Status, result.Replayed, result.Principal.Revision)
	return ExitHealthy
}
