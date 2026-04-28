package app_test

import (
	"context"
	"errors"
	"io/fs"
	"testing"

	operatorendpoints "github.com/metrofun/swobu/internal/app/operator/endpoints"
	"github.com/metrofun/swobu/internal/domain/endpointintent"
)

func TestEndpointIntentStore_PutGetListAndDeleteEndpoint(t *testing.T) {
	t.Parallel()

	repo := &fakeEndpointRepository{
		endpoints: buildEndpoints(t, testEndpointSpec{name: "alpha", selectedRef: "backend-a"}),
	}
	store := operatorendpoints.NewOperatorEndpointStore(repo)

	got, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("ListEndpoints returned error: %v", err)
	}
	if len(got) != 1 || got[0].Name().String() != "alpha" {
		t.Fatalf("listed endpoints = %#v, want [alpha]", got)
	}

	beta := buildEndpoints(t, testEndpointSpec{name: "beta", selectedRef: "backend-b"})[0]
	if _, err := store.Put(context.Background(), beta); err != nil {
		t.Fatalf("PutEndpoint returned error: %v", err)
	}
	if repo.saveCalls != 1 {
		t.Fatalf("save calls after put = %d, want 1", repo.saveCalls)
	}

	saved, err := store.Get(context.Background(), "beta")
	if err != nil {
		t.Fatalf("GetEndpoint returned error: %v", err)
	}
	if got := saved.Name().String(); got != "beta" {
		t.Fatalf("saved endpoint name = %q, want beta", got)
	}

	if err := store.Delete(context.Background(), "alpha"); err != nil {
		t.Fatalf("DeleteEndpoint returned error: %v", err)
	}
	if repo.saveCalls != 2 {
		t.Fatalf("save calls after delete = %d, want 2", repo.saveCalls)
	}
}

func TestEndpointIntentStore_GetMissingEndpointMapsNotFound(t *testing.T) {
	t.Parallel()

	store := operatorendpoints.NewOperatorEndpointStore(&fakeEndpointRepository{})
	_, err := store.Get(context.Background(), "alpha")
	if err == nil {
		t.Fatal("GetEndpoint returned nil error, want not found")
	}
	var commandErr operatorendpoints.CommandError
	if !errors.As(err, &commandErr) {
		t.Fatalf("GetEndpoint error type = %T, want EndpointCommandError", err)
	}
	if commandErr.Code != operatorendpoints.CommandNotFound {
		t.Fatalf("command error code = %q, want %q", commandErr.Code, operatorendpoints.CommandNotFound)
	}
}

type fakeEndpointRepository struct {
	endpoints []endpointintent.Endpoint
	saveCalls int
	saveErr   error
}

func (r *fakeEndpointRepository) GetEndpoint(_ context.Context, name endpointintent.EndpointName) (endpointintent.Endpoint, error) {
	for _, endpoint := range r.endpoints {
		if endpoint.Name() == name {
			return endpoint, nil
		}
	}
	return endpointintent.Endpoint{}, fs.ErrNotExist
}

func (r *fakeEndpointRepository) ListEndpoints(context.Context) ([]endpointintent.Endpoint, error) {
	return append([]endpointintent.Endpoint(nil), r.endpoints...), nil
}

func (r *fakeEndpointRepository) SaveEndpoints(_ context.Context, endpoints []endpointintent.Endpoint) error {
	if r.saveErr != nil {
		return r.saveErr
	}
	r.saveCalls++
	r.endpoints = append([]endpointintent.Endpoint(nil), endpoints...)
	return nil
}
