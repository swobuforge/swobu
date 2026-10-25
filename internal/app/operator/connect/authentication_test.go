package connect

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/swobuforge/swobu/internal/accessstate"
	accessapp "github.com/swobuforge/swobu/internal/app/operator/access"
	"github.com/swobuforge/swobu/internal/app/operator/routebindings"
	"github.com/swobuforge/swobu/internal/clientconnect"
	"github.com/swobuforge/swobu/internal/configstore"
	"github.com/swobuforge/swobu/internal/nodefile"
	"github.com/swobuforge/swobu/internal/routing"
)

type authenticationFixture struct {
	service    *Service
	authority  accessapp.Service
	state      *accessstate.Store
	node       *nodefile.File
	config     *configstore.Store
	principal  accessstate.Access
	target     clientconnect.Target
	attempts   *Attempts
	configPath string
}

func newAuthenticationFixture(t *testing.T) authenticationFixture {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", filepath.Join(dir, "codex"))
	configPath := filepath.Join(dir, "swobu.yaml")
	if err := os.WriteFile(configPath, []byte("schema_version: 5\nworkspaces: {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	config, err := configstore.Open(configPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = config.Close() })
	slug, _ := routing.ParseWorkspaceSlug("research")
	_, err = config.Update(t.Context(), func(c routing.Config) (routing.Config, error) {
		c, err := c.CreateEmptyWorkspace(routing.EmptyWorkspaceSeed{Slug: slug})
		if err != nil {
			return c, err
		}
		return c.SetServiceEnabled(slug, routing.ServiceModels, true)
	})
	if err != nil {
		t.Fatal(err)
	}
	node, err := nodefile.Open(configPath)
	if err != nil {
		t.Fatal(err)
	}
	state, err := accessstate.OpenInline(nil, node.PublishPrincipals)
	if err != nil {
		t.Fatal(err)
	}
	principal, _, err := state.CreatePrincipalChecked(t.Context(), "client", "fixture-principal", func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := state.GrantPermission(principal.ID, slug); err != nil {
		t.Fatal(err)
	}
	authority, err := accessapp.NewService(state, config, &routebindings.Coordinator{})
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(clientconnect.NewService(), authority)
	if err != nil {
		t.Fatal(err)
	}
	target, err := clientconnect.NewTarget("research", "http://127.0.0.1:7926/workspaces/research")
	if err != nil {
		t.Fatal(err)
	}
	attemptsDir := filepath.Join(dir, "attempts")
	if err := os.Mkdir(attemptsDir, 0700); err != nil {
		t.Fatal(err)
	}
	attempts, err := OpenAttempts(attemptsDir)
	if err != nil {
		t.Fatal(err)
	}
	return authenticationFixture{service: service, authority: authority, state: state, node: node, config: config, principal: principal, target: target.WithModelID("coding").WithAuthentication(principal.ID.String(), ""), attempts: attempts, configPath: filepath.Join(dir, "codex", "config.toml")}
}

func TestAuthenticatedConnectFreshSetupReuseRevocationAndHistoricalReplay(t *testing.T) {
	f := newAuthenticationFixture(t)
	preview, err := f.service.Plan(t.Context(), "codex", f.target)
	if err != nil || !preview.Authentication.IssueCredential || preview.AlreadyConfigured {
		t.Fatal("fresh preview did not require confirmed provisioning", err)
	}
	if len(f.state.List()[0].Credentials) != 0 {
		t.Fatal("preview issued a credential")
	}
	if _, err := os.Stat(f.configPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("preview wrote client configuration")
	}
	if _, err := f.service.Apply(t.Context(), "codex", preview.Review(), f.target); err == nil {
		t.Fatal("unattributed apply provisioned a credential")
	}
	const key = "authenticated-fresh-connect"
	result, err := f.service.ApplyAttempt(t.Context(), f.attempts, key, "codex", preview.Review(), f.target, func() error { return nil })
	if err != nil || !result.AlreadyConfigured || result.Replayed || result.Authentication.IssueCredential || result.Authentication.CredentialID == "" {
		t.Fatal("fresh apply failed authentication", err)
	}
	if len(f.state.List()[0].Credentials) != 1 {
		t.Fatal("fresh apply did not issue exactly once")
	}
	committed, err := os.ReadFile(f.configPath)
	if err != nil {
		t.Fatal(err)
	}
	inspect, err := clientconnect.NewService().Plan(t.Context(), clientconnect.ClientCodex, f.target)
	if err != nil {
		t.Fatal(err)
	}
	bearer := inspect.ConfiguredCredential()
	if _, err := f.authority.ClientCredential(f.principal.ID.String(), "research", bearer); err != nil {
		t.Fatal("configured token was not accepted", err)
	}
	public, _ := json.Marshal(result)
	if bytes.Contains(public, []byte(bearer)) {
		t.Fatal("Connect acknowledgement disclosed bearer")
	}
	current, err := f.service.Plan(t.Context(), "codex", f.target)
	if err != nil || !current.AlreadyConfigured || current.Authentication.IssueCredential {
		t.Fatal("valid configured credential was not reused", err)
	}
	after, _ := os.ReadFile(f.configPath)
	if !bytes.Equal(committed, after) || len(f.state.List()[0].Credentials) != 1 {
		t.Fatal("read-only reuse mutated state")
	}
	credential, _ := accessstate.ParseCredentialID(result.Authentication.CredentialID)
	if _, err := f.state.RevokeCredential(f.principal.ID, credential); err != nil {
		t.Fatal(err)
	}
	revoked, err := f.service.Plan(t.Context(), "codex", f.target)
	if err != nil || revoked.AlreadyConfigured || !revoked.Authentication.IssueCredential {
		t.Fatal("revoked credential was acknowledged as configured", err)
	}
	reconciled, err := f.service.Reconcile(t.Context(), "codex", preview.Review(), f.target)
	if err != nil || reconciled.State == "configured" {
		t.Fatal("reconciliation accepted revoked credential", err)
	}
	replay, err := f.service.ApplyAttempt(t.Context(), f.attempts, key, "codex", preview.Review(), f.target, func() error { return nil })
	if err != nil || !replay.Replayed {
		t.Fatal("original acknowledgement could not be recovered", err)
	}
	if len(f.state.List()[0].Credentials) != 0 {
		t.Fatal("historical completion replay restored a credential")
	}
}

type interruptedClient struct {
	ClientService
	afterWrite bool
}

func (c interruptedClient) Apply(ctx context.Context, plan clientconnect.Plan) (clientconnect.Plan, error) {
	if c.afterWrite {
		if _, err := c.ClientService.Apply(ctx, plan); err != nil {
			return clientconnect.Plan{}, err
		}
	}
	panic("simulated process death after issuance")
}

func TestAuthenticatedConnectLostDeliveryNeverIssuesOnRecovery(t *testing.T) {
	for _, afterWrite := range []bool{false, true} {
		t.Run(map[bool]string{false: "before-file-write", true: "after-file-write"}[afterWrite], func(t *testing.T) {
			f := newAuthenticationFixture(t)
			f.service.clients = interruptedClient{ClientService: clientconnect.NewService(), afterWrite: afterWrite}
			preview, err := f.service.Plan(t.Context(), "codex", f.target)
			if err != nil {
				t.Fatal(err)
			}
			const key = "crash-after-credential-issuance"
			func() {
				defer func() {
					if recover() == nil {
						t.Fatal("crash boundary not reached")
					}
				}()
				_, _ = f.service.ApplyAttempt(t.Context(), f.attempts, key, "codex", preview.Review(), f.target, func() error { return nil })
			}()
			// Reconstruct both durable owners rather than reusing process memory.
			principalRaw, readErr := f.node.PrincipalDocument()
			if readErr != nil {
				t.Fatal(readErr)
			}
			f.state, err = accessstate.OpenInline(principalRaw, f.node.PublishPrincipals)
			if err != nil {
				t.Fatal(err)
			}
			f.authority, err = accessapp.NewService(f.state, f.config, &routebindings.Coordinator{})
			if err != nil {
				t.Fatal(err)
			}
			f.service, err = NewService(clientconnect.NewService(), f.authority)
			if err != nil {
				t.Fatal(err)
			}
			f.attempts, err = OpenAttempts(filepath.Join(filepath.Dir(f.configPath), "..", "attempts"))
			if err != nil {
				t.Fatal(err)
			}
			credentials := f.state.List()[0].Credentials
			if len(credentials) != 1 {
				t.Fatal("issuance was not durably retained")
			}
			_, err = f.service.ApplyAttempt(t.Context(), f.attempts, key, "codex", preview.Review(), f.target, func() error { return nil })
			var recovery *CredentialDeliveryError
			if !errors.Is(err, ErrAttemptIndeterminate) || !errors.As(err, &recovery) || recovery.Credential != credentials[0].ID.String() {
				t.Fatal("recovery lost exact credential attribution", err)
			}
			if len(f.state.List()[0].Credentials) != 1 {
				t.Fatal("recovery silently issued a replacement")
			}
			reconciled, err := f.service.Reconcile(t.Context(), "codex", preview.Review(), f.target)
			if err != nil || (reconciled.State == "configured") != afterWrite {
				t.Fatal("reconciliation misrepresented authenticated partial completion", err)
			}
		})
	}
}

func TestAuthenticatedConnectPermissionMismatchCannotProvision(t *testing.T) {
	f := newAuthenticationFixture(t)
	slug, _ := routing.ParseWorkspaceSlug("research")
	if err := f.state.RevokePermission(f.principal.ID, slug); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Plan(t.Context(), "codex", f.target); err == nil {
		t.Fatal("missing permission accepted")
	}
	if len(f.state.List()[0].Credentials) != 0 {
		t.Fatal("missing permission issued credentials")
	}
}

func TestAuthenticatedConnectPermissionChangeInvalidatesConfirmedIssuance(t *testing.T) {
	f := newAuthenticationFixture(t)
	preview, err := f.service.Plan(t.Context(), "codex", f.target)
	if err != nil {
		t.Fatal(err)
	}
	slug, _ := routing.ParseWorkspaceSlug("research")
	if err := f.state.RevokePermission(f.principal.ID, slug); err != nil {
		t.Fatal(err)
	}
	_, err = f.service.ApplyAttempt(t.Context(), f.attempts, "permission-changed-before-apply", "codex", preview.Review(), f.target, func() error { return nil })
	if err == nil || len(f.state.List()[0].Credentials) != 0 {
		t.Fatal("stale permission issued a credential")
	}
	if _, err := os.Stat(f.configPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("stale permission wrote client configuration")
	}
}

func TestAuthenticatedConnectRejectsAnotherPrincipalAndAudience(t *testing.T) {
	f := newAuthenticationFixture(t)
	other, _, err := f.state.CreatePrincipalChecked(t.Context(), "other", "other-principal", func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	slug, _ := routing.ParseWorkspaceSlug("research")
	if err := f.state.GrantPermission(other.ID, slug); err != nil {
		t.Fatal(err)
	}
	issued, _, err := f.state.IssueCredentialChecked(other.ID, slug, "other-principal-token", func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.authority.ClientCredential(f.principal.ID.String(), "research", issued.Token); err == nil {
		t.Fatal("accepted another Principal's credential")
	}
	if _, err := f.authority.ClientCredential(other.ID.String(), "different", issued.Token); err == nil {
		t.Fatal("accepted another Workspace audience")
	}
}
