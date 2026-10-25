package adminmcp

import (
	"context"
	"errors"
	"time"

	"github.com/swobuforge/swobu/internal/accessstate"
	accessapp "github.com/swobuforge/swobu/internal/app/operator/access"
	"github.com/swobuforge/swobu/internal/mcp"
	"github.com/swobuforge/swobu/internal/routing"
)

func accessCommands(service accessapp.Service) []mcp.AdminTool {
	type create struct {
		Label    string `json:"label"`
		RetryKey string `json:"retry_key"`
	}
	type rename struct {
		ID       string `json:"id"`
		Label    string `json:"label"`
		Revision string `json:"revision"`
	}
	type permission struct {
		Principal     string                         `json:"principal"`
		Workspace     string                         `json:"workspace"`
		Grant         bool                           `json:"grant"`
		Services      []routing.ServiceKind          `json:"services,omitempty"`
		RequestLimits []accessstate.RequestRateLimit `json:"request_limits,omitempty"`
		ExpiresAt     *time.Time                     `json:"expires_at,omitempty"`
		DryRun        bool                           `json:"dry_run"`
		Revision      string                         `json:"revision"`
		RetryKey      string                         `json:"retry_key"`
	}
	type credentialRevocation struct {
		Principal  string `json:"principal"`
		Credential string `json:"credential"`
		DryRun     bool   `json:"dry_run"`
		Revision   string `json:"revision"`
		RetryKey   string `json:"retry_key"`
	}
	return []mcp.AdminTool{
		typedTool("principal_create", "Create Principal metadata only. Grants and credentials remain separate. Use a stable retry key scoped to your admitted identity.", objectSchema(`"label":{"type":"string"},"retry_key":{"type":"string"}`, "label", "retry_key"), func(ctx context.Context, a authority, input create) (any, error) {
			principal, replayed, err := service.CreatePrincipalChecked(ctx, input.Label, input.RetryKey, func() error { return a.check(ctx) })
			return struct {
				Principal accessapp.Principal `json:"principal"`
				Replayed  bool                `json:"replayed"`
			}{principal, replayed}, err
		}),
		typedTool("principal_rename", "Change a Principal label at the original Principal revision. Preserves grants, credentials and identity.", objectSchema(`"id":{"type":"string"},"label":{"type":"string"},"revision":{"type":"string"}`, "id", "label", "revision"), func(ctx context.Context, a authority, input rename) (any, error) {
			return service.UpdatePrincipalLabelChecked(ctx, input.ID, input.Label, input.Revision, func() error { return a.check(ctx) })
		}),
		typedTool("permission_set", "Preview a grant/revocation with dry_run=true and empty revision. Apply the same reviewed intent with dry_run=false, its original revision and a stable retry_key. Null services is an explicit broad Workspace grant. MCP access to an Admin-mounted Workspace delegates Node administration.", objectSchema(`"principal":{"type":"string"},"workspace":{"type":"string"},"grant":{"type":"boolean"},"services":{"type":["array","null"],"items":{"type":"string","enum":["models","mcp","search"]}},"request_limits":{"type":"array","items":{"type":"object","properties":{"service":{"type":"string","enum":["models","mcp","search"]},"requests_per_minute":{"type":"integer","minimum":1,"maximum":100000}},"required":["service","requests_per_minute"],"additionalProperties":false}},"expires_at":{"type":"string","format":"date-time"},"dry_run":{"type":"boolean"},"revision":{"type":"string"},"retry_key":{"type":"string"}`, "principal", "workspace", "grant", "dry_run", "revision", "retry_key"), func(ctx context.Context, a authority, input permission) (any, error) {
			var expiry []time.Time
			if input.ExpiresAt != nil {
				expiry = []time.Time{*input.ExpiresAt}
			}
			if !input.Grant && (input.Services != nil || input.RequestLimits != nil || input.ExpiresAt != nil) {
				return nil, errors.New("revocation excludes grant fields")
			}
			if input.DryRun {
				return service.PreviewPermissionServicesWithRates(input.Principal, input.Workspace, input.Revision, input.Grant, input.Services, input.RequestLimits, expiry...)
			}
			return service.ApplyPermissionServicesWithRatesChecked(ctx, input.Principal, input.Workspace, input.Revision, input.RetryKey, input.Grant, func() error { return a.check(ctx) }, input.Services, input.RequestLimits, expiry...)
		}),
		typedTool("credential_revoke", "Preview one credential's removal with dry_run=true. Apply with the original preview revision and stable retry key. Removes neither sibling credentials nor the permission. Revoking the calling credential permits this effect once.", objectSchema(`"principal":{"type":"string"},"credential":{"type":"string"},"dry_run":{"type":"boolean"},"revision":{"type":"string"},"retry_key":{"type":"string"}`, "principal", "credential", "dry_run", "revision", "retry_key"), func(ctx context.Context, a authority, input credentialRevocation) (any, error) {
			check := func() error { return a.check(ctx) }
			if input.DryRun {
				return service.PreviewCredentialRevocation(input.Principal, input.Credential, check)
			}
			return service.ApplyCredentialRevocation(ctx, input.Principal, input.Credential, input.Revision, input.RetryKey, check)
		}),
	}
}
