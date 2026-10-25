package cli

import (
	"fmt"
	"io"

	accessapp "github.com/swobuforge/swobu/internal/app/operator/access"
)

func printIssuedCredential(out io.Writer, result accessapp.IssueResult) {
	fmt.Fprintf(out, "Access ID:      %s\nCredential ID: %s\nWorkspace:     %s\nAccess key:    %s\n\nSave this key now. Swobu will not show it again.\n", result.Access.ID, result.Credential.ID, result.Credential.Workspace, result.Token)
}
