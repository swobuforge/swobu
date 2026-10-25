package mcp

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"sort"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/swobuforge/swobu/internal/connections"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
)

// OpenHTTPConnectionBinding builds the same session used by MCP checks from a
// current HTTP MCP Connection. The returned SourceBinding owns the session and
// is keyed by the Connection identity, so Workspace mounts do not need a
// source-name translation table.
func OpenHTTPConnectionBinding(ctx context.Context, value connections.Connection, resolve ConnectionCredentialResolver) (SourceBinding, error) {
	spec, ok := value.Spec().(connections.MCPHTTPSpec)
	if !ok {
		return SourceBinding{}, errors.New("HTTP MCP Connection required")
	}
	key, err := canonical.NewToolKey("mcp", canonical.ToolKindMCP, value.ID().String())
	if err != nil {
		return SourceBinding{}, err
	}
	remote, err := canonical.NewMCPURLSource(spec.Endpoint().String(), canonical.Unspecified[[]string](), canonical.NewMCPApprovalNever(), canonical.MCPLoadingEager, canonical.Unspecified[[]string]())
	if err != nil {
		return SourceBinding{}, err
	}
	declaration, err := canonical.NewMCPToolSource(key, "", remote, nil)
	if err != nil {
		return SourceBinding{}, err
	}
	source, _ := declaration.MCP()
	access := SourceAccess{}
	if spec.Credential().Kind() != credentialref.KindEmpty {
		if resolve == nil {
			return SourceBinding{}, errors.New("MCP Connection credential unavailable")
		}
		scope, err := credentialref.NewConnectionScope(value.ID().String())
		if err != nil {
			return SourceBinding{}, err
		}
		access.ResolveAuthorization = func(resolveCtx context.Context) (string, error) {
			token, err := resolve(resolveCtx, scope, spec.Credential().String())
			if err != nil || strings.TrimSpace(token) == "" {
				return "", errors.New("MCP Connection credential unavailable")
			}
			return token, nil
		}
	}
	binding, err := OpenSourceBinding(ctx, source, access)
	if err != nil {
		return SourceBinding{}, err
	}
	binding.ID = value.ID().String()
	binding.ConnectionID = value.ID().String()
	binding.ConnectionRevision = value.Revision().String()
	return binding, nil
}

// OpenStdioConnectionBinding starts one explicitly configured executable and
// binds its MCP session to the Connection identity. The child process is
// owned by the returned binding and is killed and joined when the binding is
// closed.
func OpenStdioConnectionBinding(ctx context.Context, value connections.Connection, resolve ConnectionCredentialResolver) (SourceBinding, error) {
	spec, ok := value.Spec().(connections.MCPStdioSpec)
	if !ok {
		return SourceBinding{}, errors.New("stdio MCP Connection required")
	}
	scope, err := credentialref.NewConnectionScope(value.ID().String())
	if err != nil {
		return SourceBinding{}, err
	}
	environment := spec.Environment()
	names := make([]string, 0, len(environment))
	for name := range environment {
		names = append(names, name)
	}
	sort.Strings(names)
	env := make([]string, 0, len(names))
	for _, name := range names {
		if resolve == nil {
			return SourceBinding{}, errors.New("MCP Connection credential unavailable")
		}
		secret, err := resolve(ctx, scope, environment[name].String())
		if err != nil || secret == "" || strings.ContainsRune(secret, 0) {
			return SourceBinding{}, errors.New("MCP Connection credential unavailable")
		}
		env = append(env, name+"="+secret)
	}
	command := exec.CommandContext(ctx, spec.Executable(), spec.Args()...)
	command.Env = env
	command.Stderr = io.Discard
	stdout, err := command.StdoutPipe()
	if err != nil {
		return SourceBinding{}, err
	}
	stdin, err := command.StdinPipe()
	if err != nil {
		_ = stdout.Close()
		return SourceBinding{}, err
	}
	if err := command.Start(); err != nil {
		_ = stdout.Close()
		_ = stdin.Close()
		return SourceBinding{}, err
	}
	cleanup := func() {
		_ = command.Process.Kill()
		_ = stdin.Close()
		_ = stdout.Close()
		_ = command.Wait()
	}
	transport := &sdk.IOTransport{Reader: &maxBytesReadCloser{source: stdout, remaining: MaxHTTPResponseBytes}, Writer: stdin, MaxLineLength: MaxHTTPResponseBytes}
	client := sdk.NewClient(&sdk.Implementation{Name: "swobu", Version: "v0"}, nil)
	connected, err := client.Connect(ctx, transport, nil)
	if err != nil {
		cleanup()
		return SourceBinding{}, err
	}
	key, err := canonical.NewToolKey("mcp", canonical.ToolKindMCP, value.ID().String())
	if err != nil {
		_ = connected.Close()
		cleanup()
		return SourceBinding{}, err
	}
	remote, err := canonical.NewMCPConnectorSource(value.ID().String(), canonical.Unspecified[[]string](), canonical.NewMCPApprovalNever(), canonical.MCPLoadingEager, canonical.Unspecified[[]string]())
	if err != nil {
		_ = connected.Close()
		cleanup()
		return SourceBinding{}, err
	}
	declaration, err := canonical.NewMCPToolSource(key, "", remote, nil)
	if err != nil {
		_ = connected.Close()
		cleanup()
		return SourceBinding{}, err
	}
	source, _ := declaration.MCP()
	opened := &session{source: source, sdk: connected}
	binding, err := sourceBindingFromSession(ctx, opened)
	if err != nil {
		_ = opened.close()
		cleanup()
		return SourceBinding{}, err
	}
	binding.ID = value.ID().String()
	binding.ConnectionID = value.ID().String()
	binding.ConnectionRevision = value.Revision().String()
	binding.Close = func() error {
		closeErr := opened.close()
		cleanup()
		return closeErr
	}
	return binding, nil
}
