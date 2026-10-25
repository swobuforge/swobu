package runtime

import (
	"context"
	"github.com/swobuforge/swobu/internal/adapters/outbound/credentials"
	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
	"github.com/swobuforge/swobu/internal/provider"
	"testing"
)

type scopedCredentialFixture struct {
	scope         credentialref.Scope
	ref           string
	providerCalls int
}

func (r *scopedCredentialFixture) ResolveCredential(context.Context, string, string) (string, error) {
	r.providerCalls++
	return "wrong-owner", nil
}
func (r *scopedCredentialFixture) ResolveScopedCredential(_ context.Context, scope credentialref.Scope, ref string) (string, error) {
	r.scope, r.ref = scope, ref
	return "fixture", nil
}

type providerCredentialFixture struct{}

func (providerCredentialFixture) ResolveCredential(context.Context, string, string) (string, error) {
	return "wrong-owner", nil
}

func TestResolveTargetCredentialRequiresCapturedConnectionOwner(t *testing.T) {
	id, _ := connectionidentity.Parse("cn_a")
	target, err := (provider.TargetSnapshot{ProviderSpec: "openai", CredentialRef: "env:FIXTURE"}).BindConnection(id, 2)
	if err != nil {
		t.Fatal(err)
	}
	resolver := &scopedCredentialFixture{}
	token, err := ResolveTargetCredential(t.Context(), resolver, target)
	if err != nil || token != "fixture" || resolver.scope.Kind() != credentialref.ScopeConnection || resolver.scope.Identifier() != "cn_a" || resolver.ref != target.CredentialRef || resolver.providerCalls != 0 {
		t.Fatal("captured owner was not used")
	}
	if _, err := ResolveTargetCredential(t.Context(), providerCredentialFixture{}, target); err == nil {
		t.Fatal("bound target fell back to provider owner")
	}
	if _, err := ResolveTargetCredential(t.Context(), nil, target); err == nil {
		t.Fatal("nil resolver accepted")
	}
}

func TestResolveTargetCredentialRejectsForeignStoredSlot(t *testing.T) {
	t.Setenv("SWOBU_HOME", t.TempDir())
	ref, err := credentials.StoreConnectionCredential("cn_owner", "fixture-owned", credentials.CredentialWritePolicyFile)
	if err != nil {
		t.Fatal(err)
	}
	resolver := credentials.NewResolver()
	for _, test := range []struct {
		id      string
		allowed bool
	}{{"cn_owner", true}, {"cn_other", false}} {
		id, _ := connectionidentity.Parse(test.id)
		target, err := (provider.TargetSnapshot{ProviderSpec: "openai", CredentialRef: ref}).BindConnection(id, 1)
		if err != nil {
			t.Fatal(err)
		}
		token, err := ResolveTargetCredential(t.Context(), resolver, target)
		if test.allowed {
			if err != nil || token != "fixture-owned" {
				t.Fatal("owned slot failed")
			}
		} else if err == nil {
			t.Fatal("foreign Connection resolved stored material")
		}
	}
}
