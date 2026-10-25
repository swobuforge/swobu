package mcp

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/swobuforge/swobu/internal/connections"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
)

// ConnectionCredentialResolver resolves at the consuming edge with Connection
// ownership. It never projects source/provider scope from a protocol label.
type ConnectionCredentialResolver func(context.Context, credentialref.Scope, string) (string, error)

// CheckHTTPConnection uses the same guarded SDK discovery as Workspace MCP.
// It does not install a binding, retain a catalog or modify Workspace state.
// Existing page/tool/schema/body bounds apply; the deadline covers connect and
// discovery together. Errors become closed observations, never upstream prose.
func CheckHTTPConnection(ctx context.Context, value connections.Connection, resolve ConnectionCredentialResolver) (connections.Check, error) {
	return checkHTTPConnection(ctx, value, resolve, OpenSourceBinding)
}

func checkHTTPConnection(ctx context.Context, value connections.Connection, resolve ConnectionCredentialResolver, open func(context.Context, canonical.MCPToolSource, SourceAccess) (SourceBinding, error)) (connections.Check, error) {
	spec, ok := value.Spec().(connections.MCPHTTPSpec)
	if !ok {
		return connections.Check{}, errors.New("HTTP MCP Connection required")
	}
	observe := func(reach connections.Reachability, auth connections.Authentication, capabilities []connections.Capability, failure connections.CheckError) (connections.Check, error) {
		return connections.NewCheck(value, time.Now().UTC(), reach, auth, capabilities, failure)
	}
	endpoint, err := url.Parse(spec.Endpoint().String())
	if err != nil || endpoint.Scheme != "https" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return observe(connections.ReachabilityUnknown, connections.AuthenticationUnknown, nil, connections.CheckNetworkDenied)
	}
	ctx, cancel := context.WithTimeout(ctx, DiscoveryTimeout)
	defer cancel()
	access := SourceAccess{}
	authenticated := spec.Credential().Kind() != credentialref.KindEmpty
	if authenticated {
		scope, err := credentialref.NewConnectionScope(value.ID().String())
		if err != nil {
			return connections.Check{}, err
		}
		if resolve == nil {
			return observe(connections.ReachabilityUnknown, connections.AuthenticationUnknown, nil, connections.CheckCredentialUnavailable)
		}
		token, err := resolve(ctx, scope, spec.Credential().String())
		if err != nil || strings.TrimSpace(token) == "" {
			return observe(connections.ReachabilityUnknown, connections.AuthenticationUnknown, nil, connections.CheckCredentialUnavailable)
		}
		access.ResolveAuthorization = func(ctx context.Context) (string, error) {
			token, err := resolve(ctx, scope, spec.Credential().String())
			if err != nil || strings.TrimSpace(token) == "" {
				return "", errConnectionCheckCredential
			}
			return token, nil
		}
	}
	key, err := canonical.NewToolKey("mcp", canonical.ToolKindMCP, value.ID().String())
	if err != nil {
		return connections.Check{}, err
	}
	remote, err := canonical.NewMCPURLSource(spec.Endpoint().String(), canonical.Unspecified[[]string](), canonical.NewMCPApprovalNever(), canonical.MCPLoadingEager, canonical.Unspecified[[]string]())
	if err != nil {
		return connections.Check{}, err
	}
	declaration, err := canonical.NewMCPToolSource(key, "", remote, nil)
	if err != nil {
		return connections.Check{}, err
	}
	source, _ := declaration.MCP()
	binding, err := open(ctx, source, access)
	if binding.Close != nil {
		defer binding.Close()
	}
	if err != nil {
		switch {
		case errors.Is(err, errAuthenticationRequired):
			return observe(connections.Reachable, connections.AuthenticationRejected, nil, connections.CheckAuthenticationFailed)
		case errors.Is(err, errConnectionCheckCredential):
			return observe(connections.ReachabilityUnknown, connections.AuthenticationUnknown, nil, connections.CheckCredentialUnavailable)
		case errors.Is(err, errMCPNetworkDenied):
			return observe(connections.ReachabilityUnknown, connections.AuthenticationUnknown, nil, connections.CheckNetworkDenied)
		case errors.Is(ctx.Err(), context.DeadlineExceeded), errors.Is(err, context.DeadlineExceeded):
			return observe(connections.ReachabilityUnknown, connections.AuthenticationUnknown, nil, connections.CheckTimeout)
		default:
			return observe(connections.ReachabilityUnknown, connections.AuthenticationUnknown, nil, connections.CheckUnavailable)
		}
	}
	auth := connections.AuthenticationNotRequired
	if authenticated {
		auth = connections.AuthenticationAccepted
	}
	return observe(connections.Reachable, auth, []connections.Capability{connections.CapabilityTools}, connections.CheckOK)
}

var errConnectionCheckCredential = errors.New("Connection credential unavailable during MCP check")
