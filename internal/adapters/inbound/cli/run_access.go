package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strings"

	accessapp "github.com/swobuforge/swobu/internal/app/operator/access"
	operatorclient "github.com/swobuforge/swobu/internal/app/operator/client"
	platformconfig "github.com/swobuforge/swobu/internal/platform/config"
)

func runAccess(ctx context.Context, client *http.Client, stdout, stderr io.Writer, args []string) ExitCode {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: swobu access create|list|key|revoke")
		return ExitDown
	}
	startup, err := platformconfig.ResolveStartupConfig("")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return ExitDown
	}
	if err := requireCompatibleControlPlane(ctx, client, startup.Addr); err != nil {
		fmt.Fprintln(stderr, err)
		return ExitDown
	}
	api := operatorclient.New(client, platformconfig.BaseURL(startup.Addr))
	switch args[0] {
	case "list":
		rows, err := api.ListAccess(ctx)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return ExitDown
		}
		fmt.Fprintln(stdout, "ID\tNAME\tKIND\tWORKSPACES")
		for _, row := range rows {
			workspaces := []string{}
			for _, credential := range row.Credentials {
				workspaces = append(workspaces, credential.Workspace)
			}
			scope := strings.Join(workspaces, ",")
			if row.Kind == "builtin" {
				scope = "local transport"
			}
			fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", row.ID, row.Name, row.Kind, scope)
		}
		return ExitHealthy
	case "create":
		fs := flag.NewFlagSet("access create", flag.ContinueOnError)
		fs.SetOutput(stderr)
		name := fs.String("name", "", "Access name")
		workspace := fs.String("workspace", "", "Workspace grant")
		if err := fs.Parse(args[1:]); err != nil || rejectUnexpectedPositionalArgs(fs, stderr) || *name == "" || *workspace == "" {
			fmt.Fprintln(stderr, "usage: swobu access create --name <name> --workspace <workspace>")
			return ExitDown
		}
		result, err := api.CreateAccess(ctx, *name, *workspace)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return ExitDown
		}
		printIssuedAccess(stdout, result)
		return ExitHealthy
	case "key":
		return runAccessKey(ctx, api, stdout, stderr, args[1:])
	case "revoke":
		if len(args) != 2 {
			fmt.Fprintln(stderr, "usage: swobu access revoke <access-id>")
			return ExitDown
		}
		if err := api.RevokeAccess(ctx, args[1]); err != nil {
			fmt.Fprintln(stderr, err)
			return ExitDown
		}
		fmt.Fprintf(stdout, "Revoked Access: %s\n", args[1])
		return ExitHealthy
	default:
		fmt.Fprintf(stderr, "unknown access command %q\n", args[0])
		return ExitDown
	}
}
func runAccessKey(ctx context.Context, api *operatorclient.Client, stdout, stderr io.Writer, args []string) ExitCode {
	if len(args) < 1 {
		fmt.Fprintln(stderr, "usage: swobu access key add|revoke")
		return ExitDown
	}
	switch args[0] {
	case "add":
		if len(args) < 2 {
			fmt.Fprintln(stderr, "usage: swobu access key add <access-id> --workspace <workspace>")
			return ExitDown
		}
		accessID := args[1]
		fs := flag.NewFlagSet("access key add", flag.ContinueOnError)
		fs.SetOutput(stderr)
		workspace := fs.String("workspace", "", "Workspace grant")
		if err := fs.Parse(args[2:]); err != nil || rejectUnexpectedPositionalArgs(fs, stderr) || *workspace == "" {
			fmt.Fprintln(stderr, "usage: swobu access key add <access-id> --workspace <workspace>")
			return ExitDown
		}
		result, err := api.IssueAccessCredential(ctx, accessID, *workspace)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return ExitDown
		}
		printIssuedAccess(stdout, result)
		return ExitHealthy
	case "revoke":
		if len(args) != 3 {
			fmt.Fprintln(stderr, "usage: swobu access key revoke <access-id> <credential-id>")
			return ExitDown
		}
		if err := api.RevokeAccessCredential(ctx, args[1], args[2]); err != nil {
			fmt.Fprintln(stderr, err)
			return ExitDown
		}
		fmt.Fprintf(stdout, "Revoked credential: %s\n", args[2])
		return ExitHealthy
	default:
		fmt.Fprintf(stderr, "unknown access key command %q\n", args[0])
		return ExitDown
	}
}
func printIssuedAccess(out io.Writer, result accessapp.IssueResult) {
	fmt.Fprintf(out, "Access ID:      %s\nCredential ID: %s\nWorkspace:     %s\nAccess key:    %s\n\nSave this key now. Swobu will not show it again.\n", result.Access.ID, result.Credential.ID, result.Credential.Workspace, result.Token)
}
