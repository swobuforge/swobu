package authplane

import (
	"context"
	"io/fs"
	"testing"

	operatorendpoints "github.com/swobuforge/swobu/internal/app/operator/endpoints"
	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	"github.com/swobuforge/swobu/internal/domain/protocolsurface"
	"github.com/swobuforge/swobu/internal/ports"
)

type endpointRepoStub struct {
	endpoints []endpointintent.Endpoint
}

var _ ports.EndpointIntentRepository = (*endpointRepoStub)(nil)

func (s *endpointRepoStub) ListEndpoints(_ context.Context) ([]endpointintent.Endpoint, error) {
	return append([]endpointintent.Endpoint(nil), s.endpoints...), nil
}

func (s *endpointRepoStub) SaveEndpoints(_ context.Context, endpoints []endpointintent.Endpoint) error {
	s.endpoints = append([]endpointintent.Endpoint(nil), endpoints...)
	return nil
}

func (s *endpointRepoStub) GetEndpoint(_ context.Context, name endpointintent.EndpointName) (endpointintent.Endpoint, error) {
	for _, ep := range s.endpoints {
		if ep.Name() == name {
			return ep, nil
		}
	}
	return endpointintent.Endpoint{}, fs.ErrNotExist
}

func TestEndpointCredentialRefStoreUpsertCredentialRef(t *testing.T) {
	t.Parallel()
	name, _ := endpointintent.ParseEndpointName("main")
	ref, _ := endpointintent.ParseProviderConfigRef("cfg-a")
	spec, _ := endpointintent.ParseProviderSpec("openai")
	cfg, _ := endpointintent.NewProviderConfig(ref, spec, "", "", protocolsurface.Responses)
	cfg, _ = cfg.WithModelID("gpt-4.1")
	ep, _ := endpointintent.NewEndpoint(name, []endpointintent.ProviderConfig{cfg}, ref)
	repo := &endpointRepoStub{endpoints: []endpointintent.Endpoint{ep}}
	store := NewEndpointCredentialRefStore(operatorendpoints.NewOperatorEndpointStore(repo))

	persisted, err := store.UpsertCredentialRef(context.Background(), "openai", EncodeEndpointCredentialLocator("main", "cfg-a"), "keychain:openai/default")
	if err != nil {
		t.Fatalf("UpsertCredentialRef error: %v", err)
	}
	if persisted != "keychain:openai/default" {
		t.Fatalf("persisted ref = %q", persisted)
	}
	got, err := repo.GetEndpoint(context.Background(), name)
	if err != nil {
		t.Fatalf("GetEndpoint error: %v", err)
	}
	if got.ProviderConfigs()[0].CredentialRef() != "keychain:openai/default" {
		t.Fatalf("credential ref = %q", got.ProviderConfigs()[0].CredentialRef())
	}
}

func TestEndpointCredentialRefStoreUpsertCredentialRef_SubjectLocatorSkipsEndpointMutation(t *testing.T) {
	t.Parallel()
	name, _ := endpointintent.ParseEndpointName("main")
	ref, _ := endpointintent.ParseProviderConfigRef("cfg-a")
	spec, _ := endpointintent.ParseProviderSpec("openai")
	cfg, _ := endpointintent.NewProviderConfig(ref, spec, "", "", protocolsurface.Responses)
	cfg, _ = cfg.WithModelID("gpt-4.1")
	ep, _ := endpointintent.NewEndpoint(name, []endpointintent.ProviderConfig{cfg}, ref)
	repo := &endpointRepoStub{endpoints: []endpointintent.Endpoint{ep}}
	store := NewEndpointCredentialRefStore(operatorendpoints.NewOperatorEndpointStore(repo))

	persisted, err := store.UpsertCredentialRef(context.Background(), "chatgpt", "subject:main#cfg-a", "chatgpt:acct_a")
	if err != nil {
		t.Fatalf("UpsertCredentialRef subject error: %v", err)
	}
	if persisted != "chatgpt:acct_a" {
		t.Fatalf("persisted ref = %q", persisted)
	}
	got, err := repo.GetEndpoint(context.Background(), name)
	if err != nil {
		t.Fatalf("GetEndpoint error: %v", err)
	}
	if got.ProviderConfigs()[0].CredentialRef() != "" {
		t.Fatalf("credential ref = %q, want unchanged empty ref", got.ProviderConfigs()[0].CredentialRef())
	}
}
