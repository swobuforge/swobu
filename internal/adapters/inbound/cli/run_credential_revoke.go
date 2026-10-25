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

// runCredentialRevoke requires literal selected-resource confirmation. Recovery
// retains original revision/key; current identity inspection is advisory only.
func runCredentialRevoke(ctx context.Context, client *http.Client, stdout, stderr io.Writer, args []string, runner Runner) ExitCode {
	fs := flag.NewFlagSet("credential revoke", flag.ContinueOnError)
	fs.SetOutput(stderr)
	principal := fs.String("principal", "", "credential owner Principal")
	credential := fs.String("credential", "", "selected credential to revoke")
	key := fs.String("retry-key", "", "original key for separately confirmed recovery")
	revision := fs.String("expected-revision", "", "original unquoted credential revision; required with --retry-key")
	addr := fs.String("addr", runner.Addr, "local daemon address")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: swobu credential revoke --principal <id> --credential <id> [--retry-key <key> --expected-revision <revision>]")
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
	validationRevision, validationKey := "validation-only", "validation-only"
	if recovery {
		validationRevision, validationKey = *revision, *key
	}
	if _, err := operatorclient.RestoreCredentialRevocationIntent(*principal, *credential, validationRevision, validationKey); err != nil {
		fmt.Fprintln(stderr, err)
		return ExitDown
	}
	interactive := runner.IsInteractive
	if interactive == nil {
		interactive = defaultIsInteractive
	}
	if !interactive() {
		fmt.Fprintln(stderr, "Credential revocation requires an interactive local operator; no confirmation bypass is supported.")
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
	if recovery {
		current, err := api.GetPrincipal(ctx, *principal)
		if err != nil {
			fmt.Fprintln(stderr, "Cannot inspect Principal; no revocation sent.")
			return ExitDown
		}
		metadata, _ := json.Marshal(current)
		fmt.Fprintf(stderr, "Recovery: selected revocation may already have committed or may commit for the first time. Current identity is advisory: %s\nNo current revision will replace original consent.\n", metadata)
	} else {
		preview, err := api.PreviewCredentialRevocation(ctx, *principal, *credential)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return ExitDown
		}
		var randomKey [32]byte
		if _, err := rand.Read(randomKey[:]); err != nil {
			fmt.Fprintln(stderr, "Unable to create retry key.")
			return ExitDown
		}
		*key, *revision = hex.EncodeToString(randomKey[:]), preview.Revision
		fmt.Fprintf(stderr, "Selected credential revocation\nPrincipal: %q\nCredential: %q\nWorkspace audience: %q\nCreated at: %s\nWarning: %q\n", *principal, *credential, preview.Credential.Workspace, preview.Credential.CreatedAt.Format("2006-01-02T15:04:05Z07:00"), preview.Warning)
	}
	intent, err := operatorclient.RestoreCredentialRevocationIntent(*principal, *credential, *revision, *key)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return ExitDown
	}
	confirmation := "REVOKE " + *principal + " " + *credential
	fmt.Fprintf(stderr, "Only this credential is removed. Permissions and sibling credentials remain; no replacement is issued.\nOriginal revision: %q\nRetry key: %s\nType %s to confirm this exact revocation: ", *revision, *key, confirmation)
	if runner.Stdin == nil {
		fmt.Fprintln(stderr, "Revocation cancelled; no request sent.")
		return ExitDown
	}
	line, err := bufio.NewReader(runner.Stdin).ReadString('\n')
	if err != nil || strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r") != confirmation || ctx.Err() != nil {
		fmt.Fprintln(stderr, "Revocation cancelled; no request sent.")
		return ExitDown
	}
	result, err := api.ApplyCredentialRevocation(ctx, intent)
	if err != nil {
		fmt.Fprintln(stderr, err)
		fmt.Fprintf(stderr, "Do not infer rollback or automatically retry. Retain original intent: swobu credential revoke --principal %s --credential %s --retry-key %s --expected-revision %s\n", *principal, *credential, *key, *revision)
		return ExitDown
	}
	fmt.Fprintf(stdout, "Credential %s: revoked=%t replayed=%t Principal=%s. No permission, sibling credential or replacement issuance changed by this request.\n", result.Credential, result.Revoked, result.Replayed, result.Principal.ID)
	return ExitHealthy
}
