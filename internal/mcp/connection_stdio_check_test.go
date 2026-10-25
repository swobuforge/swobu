package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/domain/connectionidentity"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/swobuforge/swobu/internal/connections"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestStdioCheckChild(t *testing.T) {
	mode := os.Getenv("SWOBU_CHECK_CHILD")
	if mode == "" {
		return
	}
	if os.Getenv("SWOBU_CHECK_PARENT_PRIVATE") != "" {
		os.Exit(3)
	}
	switch mode {
	case "stall":
		time.Sleep(time.Hour)
	case "oversize":
		os.Stdout.WriteString(strings.Repeat("x", MaxHTTPResponseBytes+1))
		os.Exit(0)
	case "valid", "execute":
		if os.Getenv("TOKEN") != "synthetic-token" {
			os.Exit(4)
		}
		server := sdk.NewServer(&sdk.Implementation{Name: "synthetic-stdio", Version: "1"}, nil)
		server.AddTool(&sdk.Tool{Name: "fixture", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
			if mode != "execute" {
				os.Exit(5)
			}
			return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "stdio result"}}}, nil
		})
		if err := server.Run(context.Background(), &sdk.StdioTransport{MaxLineLength: MaxHTTPResponseBytes}); err != nil {
			os.Exit(6)
		}
		os.Exit(0)
	}
	os.Exit(7)
}

func TestStdioConnectionMountExecutesOnlyReviewedCatalog(t *testing.T) {
	t.Setenv("SWOBU_CHECK_PARENT_PRIVATE", "must-not-inherit")
	value := stdioCheckFixture(t)
	binding, err := OpenStdioConnectionBinding(t.Context(), value, func(_ context.Context, scope credentialref.Scope, ref string) (string, error) {
		if scope.Kind() != credentialref.ScopeConnection || scope.Identifier() != value.ID().String() {
			t.Fatal("wrong credential authority")
		}
		if ref == "env:MODE" {
			return "execute", nil
		}
		return "synthetic-token", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = binding.Close() })
	slug, _ := routing.ParseWorkspaceSlug("research")
	workspace, err := routing.RestoreWorkspaceWithMCPMounts(slug, nil, nil, map[string]routing.MCPMount{
		"docs": {ConnectionID: value.ID().String(), AllowedTools: []string{"fixture"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	bindings := map[string]SourceBinding{binding.ID: binding}
	catalog, err := WorkspaceCatalog(workspace, bindings)
	if err != nil || len(catalog) != 1 || catalog[0].ConnectionID != value.ID().String() || catalog[0].ConnectionRevision != value.Revision().String() {
		t.Fatalf("catalog=%+v err=%v", catalog, err)
	}
	result, err := CallWorkspaceTool(t.Context(), workspace, bindings, catalog[0], json.RawMessage(`{}`))
	if err != nil || result == nil || len(result.Content) != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if text, ok := result.Content[0].(*sdk.TextContent); !ok || text.Text != "stdio result" {
		t.Fatalf("unexpected tool content: %+v", result.Content)
	}
	changed := binding
	changed.ConnectionRevision = "2"
	bindings[binding.ID] = changed
	if _, err := CallWorkspaceTool(t.Context(), workspace, bindings, catalog[0], json.RawMessage(`{}`)); err == nil {
		t.Fatal("stale catalog executed against a changed Connection revision")
	}
	workspace, err = routing.RestoreWorkspaceWithMCPMounts(slug, nil, nil, map[string]routing.MCPMount{
		"docs": {ConnectionID: value.ID().String(), AllowedTools: []string{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CallWorkspaceTool(t.Context(), workspace, bindings, catalog[0], json.RawMessage(`{}`)); err == nil {
		t.Fatal("removed allowlist entry still executed")
	}
}

func stdioCheckFixture(t *testing.T) connections.Connection {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	spec, err := connections.NewMCPStdioSpec(executable, []string{"-test.run=^TestStdioCheckChild$"}, map[string]credentialref.Ref{
		"SWOBU_CHECK_CHILD": credentialref.Parse("env:MODE"), "TOKEN": credentialref.Parse("env:TOKEN"),
	})
	if err != nil {
		t.Fatal(err)
	}
	id, _ := connectionidentity.New()
	value, err := connections.New(id, "Stdio", spec, time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestStdioConnectionCheckProcessDiscoveryAndBounds(t *testing.T) {
	t.Setenv("SWOBU_CHECK_PARENT_PRIVATE", "must-not-inherit")
	value := stdioCheckFixture(t)
	for _, mode := range []string{"valid", "oversize", "stall"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			if mode == "stall" {
				ctx, cancel = context.WithTimeout(t.Context(), 100*time.Millisecond)
				defer cancel()
			}
			resolve := func(_ context.Context, scope credentialref.Scope, ref string) (string, error) {
				if scope.Kind() != credentialref.ScopeConnection || scope.Identifier() != value.ID().String() {
					t.Fatal("wrong credential authority")
				}
				if ref == "env:MODE" {
					return mode, nil
				}
				return "synthetic-token", nil
			}
			started := time.Now()
			check, err := CheckStdioConnection(ctx, value, resolve)
			if err != nil {
				t.Fatal(err)
			}
			if time.Since(started) > 3*time.Second {
				t.Fatal("child was not joined within deadline")
			}
			if mode == "valid" {
				if check.Reachability() != connections.Reachable || len(check.Capabilities()) != 1 || check.Authentication() != connections.AuthenticationUnknown {
					t.Fatal("missing SDK discovery")
				}
			} else if mode == "stall" && check.ErrorCode() != connections.CheckTimeout {
				t.Fatal("stalled child did not produce timeout")
			} else if check.ErrorCode() == connections.CheckOK {
				t.Fatal("invalid child reported success")
			}
		})
	}
	check, err := CheckStdioConnection(t.Context(), value, func(context.Context, credentialref.Scope, string) (string, error) {
		return "", errors.New("private-backend")
	})
	if err != nil || check.ErrorCode() != connections.CheckCredentialUnavailable {
		t.Fatal("missing credential accepted")
	}
}

func TestOpenStdioConnectionBindingUsesConnectionScopeAndOwnsChild(t *testing.T) {
	t.Setenv("SWOBU_CHECK_PARENT_PRIVATE", "must-not-inherit")
	t.Setenv("MODE", "valid")
	value := stdioCheckFixture(t)
	binding, err := OpenStdioConnectionBinding(t.Context(), value, func(_ context.Context, scope credentialref.Scope, ref string) (string, error) {
		if scope.Kind() != credentialref.ScopeConnection || scope.Identifier() != value.ID().String() {
			t.Fatalf("wrong credential authority: %v", scope)
		}
		if ref == "env:MODE" {
			return "valid", nil
		}
		return "synthetic-token", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if binding.ID != value.ID().String() || len(binding.NativeTools) != 1 || binding.Call == nil {
		t.Fatalf("incomplete stdio binding: %#v", binding)
	}
	if err := binding.Close(); err != nil {
		t.Fatal(err)
	}
}
