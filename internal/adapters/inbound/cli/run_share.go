package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	operatorclient "github.com/swobuforge/swobu/internal/app/operator/client"
	platformconfig "github.com/swobuforge/swobu/internal/platform/config"
	"github.com/swobuforge/swobu/internal/sharestate"
)

func runShare(ctx context.Context, client *http.Client, stdout, stderr io.Writer, args []string) ExitCode {
	shareClient := *client
	if shareClient.Timeout < 3*time.Minute {
		shareClient.Timeout = 3 * time.Minute
	}
	client = &shareClient
	if len(args) > 0 && args[0] == "revoke" {
		if len(args) != 2 {
			fmt.Fprintln(stderr, "usage: swobu share revoke <workspace>/<route>")
			return ExitDown
		}
		startup, err := platformconfig.ResolveStartupConfig("")
		if err != nil {
			fmt.Fprintln(stderr, err)
			return ExitDown
		}
		if err := operatorclient.New(client, platformconfig.BaseURL(startup.Addr)).RevokeShare(ctx, args[1]); err != nil {
			fmt.Fprintln(stderr, err)
			return ExitDown
		}
		fmt.Fprintf(stdout, "Revoked: %s\n", args[1])
		return ExitHealthy
	}
	route := ""
	expiry := sharestate.ExpirySevenDays
	for index := 0; index < len(args); index++ {
		if args[index] == "--expires" && index+1 < len(args) {
			expiry = sharestate.Expiry(strings.TrimSpace(args[index+1]))
			index++
			continue
		}
		if route == "" {
			route = args[index]
			continue
		}
		fmt.Fprintf(stderr, "unexpected argument %q\n", args[index])
		return ExitDown
	}
	if route == "" {
		fmt.Fprintln(stderr, "usage: swobu share <workspace>/<route> [--expires 1d|7d|30d|never]")
		return ExitDown
	}
	startup, err := platformconfig.ResolveStartupConfig("")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return ExitDown
	}
	result, err := operatorclient.New(client, platformconfig.BaseURL(startup.Addr)).IssueShare(ctx, route, expiry)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return ExitDown
	}
	fmt.Fprintf(stdout, "Share URL: %s\nOpenAI Base URL: %s\nAnthropic Base URL: %s\nAPI key: %s\nExpiry: %s\n", result.ShareURL, result.OpenAIBaseURL, result.AnthropicBaseURL, result.APIKey, result.ExpiresAt)
	return ExitHealthy
}
