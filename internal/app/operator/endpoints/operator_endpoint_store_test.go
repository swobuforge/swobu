package endpoints

import (
	"context"
	"errors"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/endpointintent"
)

func TestPut_NilRepo(t *testing.T) {
	s := OperatorEndpointStore{repo: nil}
	ctx := context.Background()
	name, _ := endpointintent.ParseEndpointName("dev")
	ref, _ := endpointintent.ParseProviderConfigRef("cfg-0001")
	spec, _ := endpointintent.ParseProviderSpec("openai_compatible")
	cfg, _ := endpointintent.NewProviderConfig(ref, spec, "https://api.dev.com", "")
	cfg, _ = cfg.WithModelID("gpt-4")
	ep, _ := endpointintent.NewEndpoint(name, []endpointintent.ProviderConfig{cfg}, ref)
	_, err := s.Put(ctx, ep)
	var cmdErr CommandError
	if !errors.As(err, &cmdErr) || cmdErr.Code != CommandUnavailable {
		t.Fatalf("expected UNAVAILABLE, got %v", err)
	}
}

func TestPut_InvalidEndpointName(t *testing.T) {
	s, _ := storeWithFixture(t)
	ctx := context.Background()
	name := endpointintent.EndpointName{}
	ref, _ := endpointintent.ParseProviderConfigRef("cfg-0001")
	spec, _ := endpointintent.ParseProviderSpec("openai_compatible")
	cfg, _ := endpointintent.NewProviderConfig(ref, spec, "https://api.dev.com", "")
	ep, _ := endpointintent.NewEndpoint(name, []endpointintent.ProviderConfig{cfg}, ref)
	_, err := s.Put(ctx, ep)
	if err == nil {
		t.Fatal("expected error for empty endpoint name")
	}
	var cmdErr CommandError
	if !errors.As(err, &cmdErr) || cmdErr.Code != CommandInvalidArgument {
		t.Fatalf("expected INVALID_ARGUMENT, got %v", err)
	}
}

func TestPut_CreatesWithOneProviderConfig(t *testing.T) {
	s, repo := storeWithFixture(t)
	ctx := context.Background()
	name, _ := endpointintent.ParseEndpointName("new-ws")
	ref, _ := endpointintent.ParseProviderConfigRef("cfg-new-0001")
	spec, _ := endpointintent.ParseProviderSpec("openai_compatible")
	cfg, err := endpointintent.NewProviderConfig(ref, spec, "https://api.example.com", "")
	if err != nil {
		t.Fatalf("new provider config: %v", err)
	}
	cfg, err = cfg.WithModelID("gpt-4.1")
	if err != nil {
		t.Fatalf("with model: %v", err)
	}
	ep, err := endpointintent.NewEndpoint(name, []endpointintent.ProviderConfig{cfg}, ref)
	if err != nil {
		t.Fatalf("new endpoint: %v", err)
	}
	_, err = s.Put(ctx, ep)
	if err != nil {
		t.Fatalf("Put returned error: %v", err)
	}
	all := repo.mustList()
	found := false
	for _, e := range all {
		if e.Name().String() == "new-ws" {
			found = true
			if len(e.ProviderConfigs()) != 1 {
				t.Fatalf("expected 1 provider config, got %d", len(e.ProviderConfigs()))
			}
			break
		}
	}
	if !found {
		t.Fatal("new-ws endpoint not persisted")
	}
}

func TestPut_ReplacesExistingConfigs(t *testing.T) {
	s, repo := storeWithFixture(t)
	ctx := context.Background()
	existing, err := s.Get(ctx, "dev")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	name := existing.Name()
	ref, _ := endpointintent.ParseProviderConfigRef("cfg-replace-0001")
	spec, _ := endpointintent.ParseProviderSpec("openai_compatible")
	cfg, err := endpointintent.NewProviderConfig(ref, spec, "https://replaced.example.com", "sk-replaced")
	if err != nil {
		t.Fatalf("new provider config: %v", err)
	}
	cfg, err = cfg.WithModelID("gpt-4o")
	if err != nil {
		t.Fatalf("with model: %v", err)
	}
	ep, err := endpointintent.NewEndpoint(name, []endpointintent.ProviderConfig{cfg}, ref)
	if err != nil {
		t.Fatalf("new endpoint: %v", err)
	}
	_, err = s.Put(ctx, ep)
	if err != nil {
		t.Fatalf("Put returned error: %v", err)
	}
	all := repo.mustList()
	found := false
	for _, e := range all {
		if e.Name().String() == "dev" {
			found = true
			if len(e.ProviderConfigs()) != 1 {
				t.Fatalf("expected 1 config, got %d", len(e.ProviderConfigs()))
			}
			pc := e.ProviderConfigs()[0]
			if pc.ModelID() != "gpt-4o" {
				t.Fatalf("expected replaced model gpt-4o, got %q", pc.ModelID())
			}
			break
		}
	}
	if !found {
		t.Fatal("dev endpoint not persisted after update")
	}
}

func TestDelete_Success(t *testing.T) {
	s, repo := storeWithFixture(t)
	ctx := context.Background()
	if err := s.Delete(ctx, "dev"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	all := repo.mustList()
	for _, e := range all {
		if e.Name().String() == "dev" {
			t.Fatal("dev endpoint should have been deleted")
		}
	}
}

func TestDelete_NotFound(t *testing.T) {
	s, _ := storeWithFixture(t)
	ctx := context.Background()
	if err := s.Delete(ctx, "nosuch"); err == nil {
		t.Fatal("expected not found error")
	}
}

func TestDelete_NilRepo(t *testing.T) {
	s := OperatorEndpointStore{repo: nil}
	ctx := context.Background()
	err := s.Delete(ctx, "dev")
	var cmdErr CommandError
	if !errors.As(err, &cmdErr) || cmdErr.Code != CommandUnavailable {
		t.Fatalf("expected UNAVAILABLE, got %v", err)
	}
}

func TestDelete_InvalidName(t *testing.T) {
	s, _ := storeWithFixture(t)
	ctx := context.Background()
	err := s.Delete(ctx, "")
	var cmdErr CommandError
	if !errors.As(err, &cmdErr) || cmdErr.Code != CommandInvalidArgument {
		t.Fatalf("expected INVALID_ARGUMENT, got %v", err)
	}
}

// ------------------------------------------------------------------
// Test helpers
// ------------------------------------------------------------------

type memRepo struct {
	endpoints []endpointintent.Endpoint
}

func (r *memRepo) ListEndpoints(ctx context.Context) ([]endpointintent.Endpoint, error) {
	return r.endpoints, nil
}

func (r *memRepo) GetEndpoint(ctx context.Context, name endpointintent.EndpointName) (endpointintent.Endpoint, error) {
	for _, ep := range r.endpoints {
		if ep.Name() == name {
			return ep, nil
		}
	}
	return endpointintent.Endpoint{}, errors.New("not found")
}

func (r *memRepo) SaveEndpoints(ctx context.Context, list []endpointintent.Endpoint) error {
	r.endpoints = list
	return nil
}

func (r *memRepo) mustList() []endpointintent.Endpoint {
	return r.endpoints
}

func repoWithFixture() *memRepo {
	// Build one endpoint with a single provider config.
	name, _ := endpointintent.ParseEndpointName("dev")
	ref, _ := endpointintent.ParseProviderConfigRef("test-ref-0001")
	spec, _ := endpointintent.ParseProviderSpec("openai_compatible")
	cfg, _ := endpointintent.NewProviderConfig(ref, spec, "https://api.dev.com", "sk-dev")
	cfg, _ = cfg.WithModelID("gpt-4-dev")
	endpoint, _ := endpointintent.NewEndpoint(name, []endpointintent.ProviderConfig{cfg}, ref)
	return &memRepo{endpoints: []endpointintent.Endpoint{endpoint}}
}

func storeWithFixture(_ *testing.T) (OperatorEndpointStore, *memRepo) {
	repo := repoWithFixture()
	return NewOperatorEndpointStore(repo), repo
}
