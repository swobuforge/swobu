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
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	operatorclient "github.com/swobuforge/swobu/internal/app/operator/client"
	platformconfig "github.com/swobuforge/swobu/internal/platform/config"
)

// runWorkspacePermission owns literal human consent for the immutable reviewed
// relationship. Preview is not approval; failed apply never triggers rediscovery
// or another effect, and an uncertain acknowledgement does not imply rollback.
func runWorkspacePermission(ctx context.Context, client *http.Client, stdout, stderr io.Writer, args []string, runner Runner) ExitCode {
	if len(args) == 0 || (args[0] != "grant" && args[0] != "revoke") {
		fmt.Fprintln(stderr, "usage: swobu workspace permission grant|revoke --workspace <slug> --principal <id>")
		return ExitDown
	}
	operation := args[0]
	fs := flag.NewFlagSet("workspace permission "+operation, flag.ContinueOnError)
	fs.SetOutput(stderr)
	workspace := fs.String("workspace", "", "Workspace permission scope")
	principal := fs.String("principal", "", "Existing managed Principal")
	addr := fs.String("addr", runner.Addr, "local daemon address")
	expires := fs.String("expires-at", "", "grant expiry as RFC3339; omitted means no deadline")
	retryKey := fs.String("retry-key", "", "original key for separately confirmed uncertain-response recovery")
	expectedRevision := fs.String("expected-revision", "", "original unquoted permission revision; required with --retry-key")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: swobu workspace permission "+operation+" --workspace <slug> --principal <id> [--addr <host:port>]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return ExitHealthy
		}
		return ExitDown
	}
	if rejectUnexpectedPositionalArgs(fs, stderr) || *workspace == "" || *principal == "" {
		fs.Usage()
		return ExitDown
	}
	if (*retryKey == "") != (*expectedRevision == "") {
		fmt.Fprintln(stderr, "Recovery requires both --retry-key and --expected-revision from the original request.")
		return ExitDown
	}
	var expiry time.Time
	if *expires != "" {
		var err error
		expiry, err = time.Parse(time.RFC3339Nano, *expires)
		if err != nil || expiry.IsZero() || operation != "grant" {
			fmt.Fprintln(stderr, "Only grant accepts --expires-at, which must be a nonzero RFC3339 instant.")
			return ExitDown
		}
	}
	var reviewed operatorclient.ReviewedPermission
	if *retryKey != "" {
		// Recovery flags must be exact printable header/disclosure values. Do not
		// normalize altered intent or discover a substitute revision.
		for _, value := range []string{*retryKey, *expectedRevision} {
			for _, c := range value {
				if c < 33 || c > 126 || c == '"' || c == '\\' || c == ',' {
					fmt.Fprintln(stderr, "Invalid original recovery key or revision.")
					return ExitDown
				}
			}
		}
		var err error
		reviewed, err = operatorclient.RestorePermissionIntent(*workspace, *principal, operation, *expectedRevision, *retryKey, expiry)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return ExitDown
		}
	}
	interactive := runner.IsInteractive
	if interactive == nil {
		interactive = defaultIsInteractive
	}
	if !interactive() {
		fmt.Fprintln(stderr, "Permission changes require an interactive local operator; no confirmation bypass is supported.")
		return ExitDown
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
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
	key, revision := *retryKey, *expectedRevision
	var proposed json.RawMessage
	if key == "" {
		preview, err := api.PreviewWorkspacePermission(ctx, *workspace, *principal, operation == "grant", expiry)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return ExitDown
		}
		var randomKey [32]byte
		if _, err := rand.Read(randomKey[:]); err != nil {
			fmt.Fprintln(stderr, "Unable to create permission retry key.")
			return ExitDown
		}
		key, revision = hex.EncodeToString(randomKey[:]), preview.Revision
		reviewed, err = operatorclient.ReviewPermission(preview, key)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return ExitDown
		}
		// JSON quoting keeps server-owned disclosure visible without interpreting
		// terminal control characters. Permission projections contain no secrets.
		before, _ := json.Marshal(preview.Before)
		proposed, _ = json.Marshal(preview.After)
		fmt.Fprintf(stderr, "Permission %s — %s\nPrincipal: %q\nWorkspace: %q\nRevision: %q\nBefore: %s\nAfter: %s\nAffected credentials: %d\nWarning: %q\nRetry key: %s\n", preview.Operation, preview.ApprovalClass, preview.Principal, preview.Workspace, revision, before, proposed, preview.AffectedCredentials, preview.Warning, key)
	} else {
		current, err := api.GetWorkspacePermission(ctx, *workspace, *principal)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return ExitDown
		}
		state, _ := json.Marshal(current)
		proposed = json.RawMessage("null")
		if operation == "grant" {
			body := map[string]any{"workspace": *workspace, "action": "use"}
			if !expiry.IsZero() {
				body["expires_at"] = expiry.UTC()
			}
			proposed, _ = json.Marshal(body)
		}
		fmt.Fprintf(stderr, "Recover original permission %s — trusted_confirmation\nPrincipal: %q\nWorkspace: %q\nOriginal revision: %q\nOriginal proposed permission: %s\nRetry key: %s\nCurrent state (advisory): %s\nCurrent state is not a receipt or approval. No current revision will be substituted. This request affects all credentials for this Principal and Workspace; no credential is issued or erased. Retained receipts replay without another effect; expiry/eviction ends that guarantee.\n", operation, *principal, *workspace, revision, proposed, key, state)
	}
	confirmation := strings.ToUpper(operation) + " " + *principal + " " + *workspace
	fmt.Fprintf(stderr, "Type %s to confirm this exact permission change: ", confirmation)
	line, err := bufio.NewReader(runner.Stdin).ReadString('\n')
	if err != nil || strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r") != confirmation {
		fmt.Fprintln(stderr, "Permission change cancelled; no apply was sent.")
		return ExitDown
	}
	if err := ctx.Err(); err != nil {
		fmt.Fprintln(stderr, err)
		return ExitDown
	}
	result, err := api.ApplyWorkspacePermission(ctx, reviewed)
	if err != nil {
		fmt.Fprintln(stderr, err)
		fmt.Fprintf(stderr, "Apply was not acknowledged; inspect authoritative permission state. Do not infer rollback or automatically retry. Retain original operation %s, Principal %s, Workspace %s, revision %s, retry key %s and proposed permission %s for separately confirmed recovery.\nUse the same operation/scope/expiry with --retry-key and --expected-revision; never substitute a current revision.\n", operation, *principal, *workspace, revision, key, proposed)
		return ExitDown
	}
	permission, _ := json.Marshal(result.Permission)
	fmt.Fprintf(stdout, "Permission apply acknowledged: Principal %s, Workspace %s, revision %s, allowed=%t, replayed=%t, permission=%s.\n", result.Principal, result.Workspace, result.Revision, result.Allowed, result.Replayed, permission)
	return ExitHealthy
}
