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
	platformconfig "github.com/swobuforge/swobu/internal/platform/config"
)

// runCredential retains one exact issuance request and requires fresh local
// consent. Recovery may mint only when the original intent never committed;
// committed replay never prints a token or claims successful secret delivery.
func runCredential(ctx context.Context, client *http.Client, stdout, stderr io.Writer, args []string, runner Runner) ExitCode {
	if len(args) > 0 && args[0] == "revoke" {
		return runCredentialRevoke(ctx, client, stdout, stderr, args[1:], runner)
	}
	if len(args) == 0 || args[0] != "issue" {
		fmt.Fprintln(stderr, "usage: swobu credential issue --principal <id> --workspace <slug> | revoke --principal <id> --credential <id>")
		return ExitDown
	}
	fs := flag.NewFlagSet("credential issue", flag.ContinueOnError)
	fs.SetOutput(stderr)
	principal := fs.String("principal", "", "existing managed Principal")
	workspace := fs.String("workspace", "", "credential audience Workspace")
	addr := fs.String("addr", runner.Addr, "local daemon address")
	retry := fs.String("retry-key", "", "original issuance key for separately confirmed recovery")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: swobu credential issue --principal <id> --workspace <slug> [--retry-key <original-key>]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return ExitHealthy
		}
		return ExitDown
	}
	if rejectUnexpectedPositionalArgs(fs, stderr) || *principal == "" || *workspace == "" {
		fs.Usage()
		return ExitDown
	}
	key := *retry
	if key == "" {
		var randomKey [32]byte
		if _, err := rand.Read(randomKey[:]); err != nil {
			fmt.Fprintln(stderr, "Unable to generate issuance key.")
			return ExitDown
		}
		key = hex.EncodeToString(randomKey[:])
	}
	// Restrict recovery disclosure to printable header characters; never interpret
	// user-supplied control sequences or silently normalize a changed key.
	for _, c := range key {
		if c < 33 || c > 126 {
			fmt.Fprintln(stderr, "Invalid original issuance key.")
			return ExitDown
		}
	}
	intent, err := operatorclient.RestoreCredentialIssuanceIntent(*principal, *workspace, key)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return ExitDown
	}
	interactive := runner.IsInteractive
	if interactive == nil {
		interactive = defaultIsInteractive
	}
	if !interactive() {
		fmt.Fprintln(stderr, "Credential issuance requires an interactive local operator; no confirmation bypass is supported.")
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
	current, err := api.GetWorkspacePermission(ctx, *workspace, *principal)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return ExitDown
	}
	fmt.Fprintf(stderr, "Credential issuance — trusted_confirmation\nPrincipal: %q\nWorkspace audience: %q\nRetry key: %s\nCurrent permission allowed=%t (advisory; authority is rechecked at issuance).\n", *principal, *workspace, key, current.Allowed)
	if *retry != "" {
		fmt.Fprintln(stderr, "Recovering the original request: if committed, returns metadata only with secret_not_recoverable. If not committed, this may issue the first credential. Do not replace the original key or infer rollback. A lost secret requires explicit revocation and a separately confirmed new issuance; recovery never rotates automatically.")
	}
	confirmation := "ISSUE " + *principal + " " + *workspace
	fmt.Fprintf(stderr, "Type %s to confirm this exact issuance: ", confirmation)
	if runner.Stdin == nil {
		fmt.Fprintln(stderr, "Issuance cancelled; no request sent.")
		return ExitDown
	}
	line, err := bufio.NewReader(runner.Stdin).ReadString('\n')
	if err != nil || strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r") != confirmation {
		fmt.Fprintln(stderr, "Issuance cancelled; no request sent.")
		return ExitDown
	}
	if err := ctx.Err(); err != nil {
		fmt.Fprintln(stderr, err)
		return ExitDown
	}
	result, err := api.ApplyCredentialIssuance(ctx, intent)
	if err != nil {
		fmt.Fprintln(stderr, err)
		fmt.Fprintf(stderr, "Issuance was not acknowledged; do not infer rollback or automatically retry. Retain original Principal %s, Workspace %s and retry key %s. Recover with swobu credential issue --principal %s --workspace %s --retry-key %s after fresh confirmation.\n", *principal, *workspace, key, *principal, *workspace, key)
		return ExitDown
	}
	if result.SecretNotRecoverable {
		fmt.Fprintf(stdout, "Original issuance acknowledged: Principal %s, Workspace %s, credential %s; secret_not_recoverable=true. No secret was delivered by this recovery. Revoke this credential explicitly before separately confirmed replacement issuance.\n", *principal, *workspace, result.Credential.ID)
		return ExitHealthy
	}
	printIssuedCredential(stdout, result)
	return ExitHealthy
}
