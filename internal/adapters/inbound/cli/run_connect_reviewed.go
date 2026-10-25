package cli

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	operatorclient "github.com/swobuforge/swobu/internal/app/operator/client"
	platformconfig "github.com/swobuforge/swobu/internal/platform/config"
	"github.com/swobuforge/swobu/internal/routing"
)

// runReviewedConnect publishes original review privately before one confirmed
// Control send. Recovery never previews again or substitutes current consent.
func runReviewedConnect(ctx context.Context, httpClient *http.Client, stdout, stderr io.Writer, runner Runner, workspace, clientID, principal, modelID, addr, path string, recover bool) ExitCode {
	interactive := runner.IsInteractive
	if interactive == nil {
		interactive = defaultIsInteractive
	}
	if !interactive() || runner.Stdin == nil {
		fmt.Fprintln(stderr, "Reviewed Connect requires an interactive local operator; no confirmation bypass is supported.")
		return ExitDown
	}
	slug, err := routing.ParseWorkspaceSlug(workspace)
	if err != nil || slug.String() != workspace {
		fmt.Fprintln(stderr, "Reviewed Connect requires an exact --workspace slug.")
		return ExitDown
	}
	store, err := openConnectRecovery(path)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return ExitDown
	}
	defer store.root.Close()
	intent, loadErr := store.load()
	if (recover && loadErr != nil) || (!recover && !errors.Is(loadErr, os.ErrNotExist)) {
		fmt.Fprintln(stderr, "Original Connect record unavailable or already retained; use --recover for an existing record. No apply sent.")
		return ExitDown
	}
	if recover {
		review, err := intent.Review()
		if err != nil || review.Workspace != workspace || review.ModelID != modelID || review.Client.ID != clientID || review.Authentication.Principal != principal {
			fmt.Fprintln(stderr, "Original Connect record does not match the selected Workspace/client; no apply sent.")
			return ExitDown
		}
	}
	startup, err := platformconfig.ResolveStartupConfig(addr)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return ExitDown
	}
	if err := requireCompatibleControlPlane(ctx, httpClient, startup.Addr); err != nil {
		fmt.Fprintln(stderr, err)
		return ExitDown
	}
	api := operatorclient.New(httpClient, platformconfig.BaseURL(startup.Addr))
	if !recover {
		plan, err := api.PreviewClientConfiguration(ctx, workspace, clientID, principal, modelID)
		if err != nil {
			fmt.Fprintf(stderr, "Connect review unavailable; no apply sent: %v\n", err)
			return ExitDown
		}
		var key [32]byte
		if _, err := rand.Read(key[:]); err != nil {
			fmt.Fprintln(stderr, "Cannot issue Connect retry key.")
			return ExitDown
		}
		intent, err = operatorclient.NewClientConfigurationIntent(plan, hex.EncodeToString(key[:]))
		if err != nil {
			fmt.Fprintln(stderr, "Cannot retain Connect review; no apply sent.")
			return ExitDown
		}
	}
	review, err := intent.Review()
	if err != nil {
		fmt.Fprintln(stderr, "Original Connect review unavailable; no apply sent.")
		return ExitDown
	}
	fmt.Fprintf(stderr, "Original Connect intent: client=%s Workspace=%s revision=%q\n", clientID, workspace, review.Revision)
	fmt.Fprintf(stderr, "Principal: %s; credential provisioning required: %t\n", review.Authentication.Principal, review.Authentication.IssueCredential)
	for _, path := range review.ConfigPaths {
		fmt.Fprintf(stderr, "File: %q\n", path)
	}
	for _, change := range review.Changes {
		fmt.Fprintf(stderr, "%q: %q -> %q (previously present: %t)\n", change.Field, change.Before, change.After, change.BeforeExists)
	}
	if recover {
		fmt.Fprintln(stderr, "Recovery uses original intent, not current readiness. Completed replay does not rewrite files; an unstarted original attempt may still apply. Indeterminate attempts cannot redispatch.")
	}
	confirmation := "CONNECT " + clientID + " " + workspace
	fmt.Fprintf(stderr, "Private before-values remain local. Replacement required: %t. Type %s to confirm: ", review.RequiresReplace, confirmation)
	line, err := bufio.NewReader(runner.Stdin).ReadString('\n')
	if err != nil || strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r") != confirmation {
		fmt.Fprintln(stderr, "Connect cancelled; no apply sent.")
		return ExitDown
	}
	if err := ctx.Err(); err != nil {
		fmt.Fprintln(stderr, "Connect cancelled; no apply sent.")
		return ExitDown
	}
	if err := store.retain(intent); err != nil {
		fmt.Fprintln(stderr, err)
		return ExitDown
	}
	result, err := api.ApplyClientConfiguration(ctx, intent)
	if err != nil {
		fmt.Fprintln(stderr, err)
		fmt.Fprintf(stderr, "Connect not acknowledged; do not infer rollback or retry automatically. Original private intent remains at %q; use --recover with fresh confirmation.\n", path)
		return ExitDown
	}
	if result.Replayed {
		fmt.Fprintln(stdout, "Recovered original Connect completion; current client configuration was not inspected or changed.")
	} else {
		fmt.Fprintln(stdout, "configured (verified original Connect apply)")
	}
	return ExitHealthy
}
