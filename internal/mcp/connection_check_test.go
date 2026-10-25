package mcp

import (
	"context"
	"errors"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/domain/connectionidentity"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/swobuforge/swobu/internal/connections"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
)

func checkConnection(t *testing.T, endpoint, ref string) connections.Connection {
	t.Helper()
	id, _ := connectionidentity.New()
	parsed, err := connections.ParseEndpoint(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := connections.NewMCPHTTPSpec(parsed, credentialref.Parse(ref))
	if err != nil {
		t.Fatal(err)
	}
	value, err := connections.New(id, "Shared", spec, time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestConnectionMCPCheckUsesScopedCredentialAndClosesDiscovery(t *testing.T) {
	value := checkConnection(t, "https://example.com/mcp", "env:SYNTHETIC")
	resolved, closed := 0, 0
	resolve := func(_ context.Context, scope credentialref.Scope, ref string) (string, error) {
		resolved++
		if scope.Kind() != credentialref.ScopeConnection || scope.Identifier() != value.ID().String() || ref != "env:SYNTHETIC" {
			t.Fatal("wrong credential authority")
		}
		return "synthetic-token", nil
	}
	open := func(ctx context.Context, source canonical.MCPToolSource, access SourceAccess) (SourceBinding, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > DiscoveryTimeout {
			t.Fatal("unbounded discovery")
		}
		token, err := access.ResolveAuthorization(ctx)
		if err != nil || token != "synthetic-token" {
			t.Fatal("credential not live")
		}
		if source.Key().Name() != value.ID().String() {
			t.Fatal("wrong source identity")
		}
		return SourceBinding{Close: func() error { closed++; return nil }}, nil
	}
	check, err := checkHTTPConnection(t.Context(), value, resolve, open)
	if err != nil || check.Authentication() != connections.AuthenticationAccepted || check.Reachability() != connections.Reachable || len(check.Capabilities()) != 1 || resolved != 2 || closed != 1 {
		t.Fatal("incorrect success observation", err)
	}
}

func TestConnectionMCPCheckSanitizesFailuresAndNeverOpensMissingCredential(t *testing.T) {
	value := checkConnection(t, "https://example.com/mcp", "")
	for _, test := range []struct {
		err     error
		failure connections.CheckError
		auth    connections.Authentication
	}{
		{errAuthenticationRequired, connections.CheckAuthenticationFailed, connections.AuthenticationRejected},
		{errMCPNetworkDenied, connections.CheckNetworkDenied, connections.AuthenticationUnknown},
		{context.DeadlineExceeded, connections.CheckTimeout, connections.AuthenticationUnknown},
		{errors.New("synthetic-private-provider-body"), connections.CheckUnavailable, connections.AuthenticationUnknown},
	} {
		closed := false
		check, err := checkHTTPConnection(t.Context(), value, nil, func(context.Context, canonical.MCPToolSource, SourceAccess) (SourceBinding, error) {
			return SourceBinding{Close: func() error { closed = true; return nil }}, test.err
		})
		if err != nil || check.ErrorCode() != test.failure || check.Authentication() != test.auth || !closed {
			t.Fatal("incorrect failure observation", err)
		}
	}
	credentialed := checkConnection(t, "https://example.com/mcp", "env:SYNTHETIC")
	opened := false
	check, err := checkHTTPConnection(t.Context(), credentialed, func(context.Context, credentialref.Scope, string) (string, error) { return "", errors.New("private") }, func(context.Context, canonical.MCPToolSource, SourceAccess) (SourceBinding, error) {
		opened = true
		return SourceBinding{}, nil
	})
	if err != nil || opened || check.ErrorCode() != connections.CheckCredentialUnavailable {
		t.Fatal("opened without credential")
	}
	insecure := checkConnection(t, "http://127.0.0.1/mcp", "")
	check, err = CheckHTTPConnection(t.Context(), insecure, nil)
	if err != nil || check.ErrorCode() != connections.CheckNetworkDenied {
		t.Fatal("unsafe scheme admitted")
	}
}

func TestConnectionMCPCheckGuardedClientRejectsPrivateDirectAndRedirects(t *testing.T) {
	// Explicit direct routing keeps this proof independent of host proxy policy.
	t.Setenv("NO_PROXY", "*")
	value := checkConnection(t, "https://127.0.0.1:1/mcp", "")
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	check, err := CheckHTTPConnection(ctx, value, nil)
	if err != nil || check.ErrorCode() != connections.CheckNetworkDenied {
		t.Fatalf("private endpoint observation=%v err=%v", check.ErrorCode(), err)
	}
	key, _ := canonical.NewToolKey("mcp", canonical.ToolKindMCP, "fixture")
	remote, _ := canonical.NewMCPURLSource("https://example.com/mcp", canonical.Unspecified[[]string](), canonical.NewMCPApprovalNever(), canonical.MCPLoadingEager, canonical.Unspecified[[]string]())
	declaration, _ := canonical.NewMCPToolSource(key, "", remote, nil)
	source, _ := declaration.MCP()
	client, _, err := safeHTTPClient(source, SourceAccess{})
	if err != nil {
		t.Fatal(err)
	}
	destination, _ := url.Parse("https://127.0.0.1/private")
	if !errors.Is(client.CheckRedirect(&http.Request{URL: destination}, nil), http.ErrUseLastResponse) {
		t.Fatal("redirect followed")
	}
	if publicMCPResolution([]netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("::ffff:127.0.0.1")}) {
		t.Fatal("mixed resolution admitted")
	}
	if strings.Contains(string(check.ErrorCode()), "127.0.0.1") {
		t.Fatal("endpoint leaked into observation")
	}
}

func TestConnectionCheckDiscoversThroughOfficialSDKSession(t *testing.T) {
	value := checkConnection(t, "https://example.com/mcp", "")
	check, err := checkHTTPConnection(t.Context(), value, nil, func(ctx context.Context, source canonical.MCPToolSource, _ SourceAccess) (SourceBinding, error) {
		serverTransport, clientTransport := sdk.NewInMemoryTransports()
		server := sdk.NewServer(&sdk.Implementation{Name: "synthetic-check", Version: "1"}, nil)
		server.AddTool(&sdk.Tool{Name: "fixture", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
			t.Fatal("check executed a tool")
			return nil, nil
		})
		serverSession, err := server.Connect(ctx, serverTransport, nil)
		if err != nil {
			return SourceBinding{}, err
		}
		t.Cleanup(func() { serverSession.Close() })
		client := sdk.NewClient(&sdk.Implementation{Name: "synthetic-client", Version: "1"}, nil)
		sessionClient, err := client.Connect(ctx, clientTransport, nil)
		if err != nil {
			return SourceBinding{}, err
		}
		binding, err := sourceBindingFromSession(ctx, &session{source: source, sdk: sessionClient})
		if err == nil && len(binding.NativeTools) != 1 {
			t.Fatal("discovery did not enumerate real SDK tools")
		}
		return binding, err
	})
	if err != nil || check.Reachability() != connections.Reachable || check.Authentication() != connections.AuthenticationNotRequired || len(check.Capabilities()) != 1 {
		t.Fatal("SDK discovery observation failed", err)
	}
}
