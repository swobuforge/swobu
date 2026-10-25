package mcp

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"sort"
	"strings"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/swobuforge/swobu/internal/connections"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
)

// CheckStdioConnection runs the explicit local executable/argv without a shell.
// Only declared environment bindings reach the child; daemon environment and
// stderr never enter the protocol or operator result. A check discovers tools,
// never invokes them. The child is killed and joined before this call returns.
func CheckStdioConnection(ctx context.Context, value connections.Connection, resolve ConnectionCredentialResolver) (connections.Check, error) {
	spec, ok := value.Spec().(connections.MCPStdioSpec)
	if !ok {
		return connections.Check{}, errors.New("stdio MCP Connection required")
	}
	ctx, cancel := context.WithTimeout(ctx, DiscoveryTimeout)
	defer cancel()
	observe := func(reach connections.Reachability, capabilities []connections.Capability, failure connections.CheckError) (connections.Check, error) {
		return connections.NewCheck(value, time.Now().UTC(), reach, connections.AuthenticationUnknown, capabilities, failure)
	}
	scope, err := credentialref.NewConnectionScope(value.ID().String())
	if err != nil {
		return connections.Check{}, err
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
			return observe(connections.ReachabilityUnknown, nil, connections.CheckCredentialUnavailable)
		}
		secret, err := resolve(ctx, scope, environment[name].String())
		if err != nil || secret == "" || strings.ContainsRune(secret, 0) {
			return observe(connections.ReachabilityUnknown, nil, connections.CheckCredentialUnavailable)
		}
		env = append(env, name+"="+secret)
	}
	command := exec.CommandContext(ctx, spec.Executable(), spec.Args()...)
	command.Env = env
	command.Stderr = io.Discard
	stdout, err := command.StdoutPipe()
	if err != nil {
		return observe(connections.ReachabilityUnknown, nil, connections.CheckUnavailable)
	}
	stdin, err := command.StdinPipe()
	if err != nil {
		stdout.Close()
		return observe(connections.ReachabilityUnknown, nil, connections.CheckUnavailable)
	}
	if err := command.Start(); err != nil {
		stdout.Close()
		stdin.Close()
		return observe(connections.ReachabilityUnknown, nil, connections.CheckUnavailable)
	}
	// Install cleanup before SDK setup so failed initialization has the same
	// ownership as successful discovery. Kill also unblocks pipes on cancellation.
	defer func() { command.Process.Kill(); stdin.Close(); stdout.Close(); command.Wait() }()
	transport := &sdk.IOTransport{Reader: &maxBytesReadCloser{source: stdout, remaining: MaxHTTPResponseBytes}, Writer: stdin, MaxLineLength: MaxHTTPResponseBytes}
	client := sdk.NewClient(&sdk.Implementation{Name: "swobu", Version: "v0"}, nil)
	connected, err := client.Connect(ctx, transport, nil)
	if err != nil {
		if ctx.Err() != nil {
			return observe(connections.ReachabilityUnknown, nil, connections.CheckTimeout)
		}
		return observe(connections.ReachabilityUnknown, nil, connections.CheckInvalidResponse)
	}
	// Kill first: an uncooperative child must not extend SDK session shutdown.
	defer func() { command.Process.Kill(); connected.Close() }()
	key, err := canonical.NewToolKey("mcp", canonical.ToolKindMCP, value.ID().String())
	if err != nil {
		return connections.Check{}, err
	}
	remote, err := canonical.NewMCPConnectorSource(value.ID().String(), canonical.Unspecified[[]string](), canonical.NewMCPApprovalNever(), canonical.MCPLoadingEager, canonical.Unspecified[[]string]())
	if err != nil {
		return connections.Check{}, err
	}
	declaration, err := canonical.NewMCPToolSource(key, "", remote, nil)
	if err != nil {
		return connections.Check{}, err
	}
	source, _ := declaration.MCP()
	opened := &session{source: source, sdk: connected}
	if _, _, err := opened.listTools(ctx); err != nil {
		if ctx.Err() != nil {
			return observe(connections.Reachable, nil, connections.CheckTimeout)
		}
		return observe(connections.Reachable, nil, connections.CheckInvalidResponse)
	}
	return observe(connections.Reachable, []connections.Capability{connections.CapabilityTools}, connections.CheckOK)
}
