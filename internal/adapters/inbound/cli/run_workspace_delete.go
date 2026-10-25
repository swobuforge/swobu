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
	"os"
	"os/signal"
	"strings"
	"syscall"

	operatorclient "github.com/swobuforge/swobu/internal/app/operator/client"
	platformconfig "github.com/swobuforge/swobu/internal/platform/config"
)

// runWorkspaceDelete owns trusted human consent, never model approval. Consent
// binds the displayed server preview and one stable retry key; it cannot upgrade
// stale state, and a failed absence readback never implies rollback.
func runWorkspaceDelete(ctx context.Context, client *http.Client, stdout, stderr io.Writer, args []string, runner Runner) ExitCode {
	fs := flag.NewFlagSet("workspace delete", flag.ContinueOnError)
	fs.SetOutput(stderr)
	slug := fs.String("workspace", "", "Workspace to permanently delete")
	addr := fs.String("addr", runner.Addr, "local daemon address")
	retryKey := fs.String("retry-key", "", "original idempotency key for separately confirmed uncertain-response recovery")
	expectedETag := fs.String("expected-etag", "", "original opaque ETag, unquoted; required with --retry-key")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: swobu workspace delete --workspace <slug> [--addr <host:port>]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return ExitHealthy
		}
		return ExitDown
	}
	if rejectUnexpectedPositionalArgs(fs, stderr) || strings.TrimSpace(*slug) == "" {
		fs.Usage()
		return ExitDown
	}
	if (*retryKey == "") != (*expectedETag == "") {
		fmt.Fprintln(stderr, "Recovery requires both --retry-key and --expected-etag from the original request.")
		return ExitDown
	}
	if *retryKey != "" {
		// Recovery values are opaque, but must be safe both as exact HTTP
		// preconditions and as terminal disclosure. Do not trim altered intent.
		valid := len(*retryKey) <= 256
		for _, value := range []string{*retryKey, *expectedETag} {
			for _, c := range value {
				if c < 33 || c > 126 || c == '"' || c == '\\' || c == ',' {
					valid = false
				}
			}
		}
		if !valid {
			fmt.Fprintln(stderr, "Invalid recovery key or unquoted ETag; use the exact original printable values.")
			return ExitDown
		}
	}
	interactive := runner.IsInteractive
	if interactive == nil {
		interactive = defaultIsInteractive
	}
	if !interactive() {
		fmt.Fprintln(stderr, "Workspace deletion requires an interactive local operator; no confirmation bypass is supported.")
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
	workspace, etag, key := strings.TrimSpace(*slug), *expectedETag, *retryKey
	if key == "" {
		preview, err := api.PreviewWorkspaceDeletion(ctx, workspace)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return ExitDown
		}
		workspace, etag = preview.Workspace, preview.ManagementETag
		var randomKey [32]byte
		if _, err := rand.Read(randomKey[:]); err != nil {
			fmt.Fprintln(stderr, "Unable to create deletion retry key.")
			return ExitDown
		}
		key = hex.EncodeToString(randomKey[:])
		fmt.Fprintf(stderr, "Delete Workspace %s — %s\nRevision: %d\nETag: %s\nRoutes: %d\n%s\nRecovery: %s\nRetry key: %s\n", workspace, preview.ApprovalClass, preview.ManagementRevision, etag, preview.RouteCount, preview.Diff, preview.Warning, key)
	} else {
		current, err := api.GetWorkspace(ctx, workspace)
		state := "absent"
		if err == nil {
			state = "present with original ETag; retry may complete the original deletion"
			if current.ManagementETag != etag {
				state = "present with changed lifetime/revision; original request cannot delete it"
			}
		} else if !operatorclient.IsNotFound(err) {
			fmt.Fprintln(stderr, err)
			return ExitDown
		}
		// A read cannot establish a receipt. Retain original intent even when
		// absent or recreated; only the routing transaction resolves replay.
		fmt.Fprintf(stderr, "Recover original deletion of Workspace %s — high_risk\nOriginal ETag: %s\nRetry key: %s\nCurrent state: %s\nAccess credential revocation is independently durable; recovery does not restore credentials.\nRetained receipts replay without another effect; expiry/eviction ends that guarantee. No current ETag will be substituted.\n", workspace, etag, key, state)
	}
	confirmation := "DELETE " + workspace
	fmt.Fprintf(stderr, "Type %s to confirm this exact deletion: ", confirmation)
	line, err := bufio.NewReader(runner.Stdin).ReadString('\n')
	if err != nil || strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r") != confirmation {
		fmt.Fprintln(stderr, "Deletion cancelled; no apply was sent.")
		return ExitDown
	}
	if err := ctx.Err(); err != nil {
		fmt.Fprintln(stderr, err)
		return ExitDown
	}
	if err := api.ApplyWorkspaceDeletion(ctx, workspace, etag, key); err != nil {
		fmt.Fprintln(stderr, err)
		fmt.Fprintf(stderr, "Inspect authoritative state. Keep retry key %s and ETag %s for uncertain-response recovery; do not treat this as rollback.\nUse workspace delete with --workspace, --retry-key and --expected-etag to separately confirm the original request.\n", key, etag)
		return ExitDown
	}
	_, err = api.GetWorkspace(ctx, workspace)
	var command *operatorclient.ResponseError
	if !errors.As(err, &command) || command.StatusCode != http.StatusNotFound {
		fmt.Fprintln(stderr, "Deletion committed but absence is not verified (readback unavailable or Workspace recreated). Do not infer rollback.")
		return ExitDown
	}
	fmt.Fprintf(stdout, "Workspace %s deleted; authoritative absence verified.\n", workspace)
	return ExitHealthy
}
