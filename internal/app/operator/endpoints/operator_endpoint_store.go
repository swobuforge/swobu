package endpoints

import (
	"context"
	"errors"
	"io/fs"

	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	"github.com/swobuforge/swobu/internal/ports"
)

type CommandErrorCode string

const (
	CommandInvalidArgument CommandErrorCode = "INVALID_ARGUMENT"
	CommandNotFound        CommandErrorCode = "NOT_FOUND"
	CommandConflict        CommandErrorCode = "CONFLICT"
	CommandUnavailable     CommandErrorCode = "UNAVAILABLE"
	CommandInternal        CommandErrorCode = "INTERNAL"
)

// CommandError preserves endpoint-intent command and query failures across
// inbound adapters so operator surfaces map one daemon truth rather than
// inventing their own persistence or validation categories.
type CommandError struct {
	Code    CommandErrorCode
	Message string
	Err     error
}

func (e CommandError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return "endpoint command failed"
}

func (e CommandError) Unwrap() error { return e.Err }

// OperatorEndpointStore owns daemon-side endpoint-intent operator queries and writes. TUI,
// CLI, and future WebUI should depend on this seam via the daemon control plane
// rather than talking to endpoint storage directly.
type OperatorEndpointStore struct {
	repo ports.EndpointIntentRepository
}

func NewOperatorEndpointStore(repo ports.EndpointIntentRepository) OperatorEndpointStore {
	return OperatorEndpointStore{repo: repo}
}

func (s OperatorEndpointStore) List(ctx context.Context) ([]endpointintent.Endpoint, error) {
	if s.repo == nil {
		return nil, CommandError{
			Code:    CommandUnavailable,
			Message: "endpoint control plane is unavailable",
		}
	}
	endpoints, err := s.repo.ListEndpoints(ctx)
	if err != nil {
		return nil, CommandError{
			Code:    CommandInternal,
			Message: "endpoint catalog could not be loaded",
			Err:     err,
		}
	}
	return endpoints, nil
}

func (s OperatorEndpointStore) Get(ctx context.Context, name string) (endpointintent.Endpoint, error) {
	if s.repo == nil {
		return endpointintent.Endpoint{}, CommandError{
			Code:    CommandUnavailable,
			Message: "endpoint control plane is unavailable",
		}
	}
	parsed, err := endpointintent.ParseEndpointName(name)
	if err != nil {
		return endpointintent.Endpoint{}, CommandError{
			Code:    CommandInvalidArgument,
			Message: err.Error(),
			Err:     err,
		}
	}
	endpoint, err := s.repo.GetEndpoint(ctx, parsed)
	if err != nil {
		return endpointintent.Endpoint{}, mapRepoError(err)
	}
	return endpoint, nil
}

func (s OperatorEndpointStore) Put(ctx context.Context, endpoint endpointintent.Endpoint) (endpointintent.Endpoint, error) {
	if s.repo == nil {
		return endpointintent.Endpoint{}, CommandError{
			Code:    CommandUnavailable,
			Message: "endpoint control plane is unavailable",
		}
	}
	if endpoint.Name().IsZero() {
		return endpointintent.Endpoint{}, CommandError{
			Code:    CommandInvalidArgument,
			Message: "endpoint name is required",
		}
	}
	endpoints, err := s.repo.ListEndpoints(ctx)
	if err != nil {
		return endpointintent.Endpoint{}, CommandError{
			Code:    CommandInternal,
			Message: "endpoint catalog could not be loaded",
			Err:     err,
		}
	}
	replaced := false
	for i, existing := range endpoints {
		if existing.Name() != endpoint.Name() {
			continue
		}
		endpoints[i] = endpoint
		replaced = true
		break
	}
	if !replaced {
		endpoints = append(endpoints, endpoint)
	}
	if err := s.repo.SaveEndpoints(ctx, endpoints); err != nil {
		return endpointintent.Endpoint{}, CommandError{
			Code:    CommandInternal,
			Message: "endpoint could not be saved",
			Err:     err,
		}
	}
	return endpoint, nil
}

func (s OperatorEndpointStore) Delete(ctx context.Context, name string) error {
	if s.repo == nil {
		return CommandError{
			Code:    CommandUnavailable,
			Message: "endpoint control plane is unavailable",
		}
	}
	parsed, err := endpointintent.ParseEndpointName(name)
	if err != nil {
		return CommandError{
			Code:    CommandInvalidArgument,
			Message: err.Error(),
			Err:     err,
		}
	}
	endpoints, err := s.repo.ListEndpoints(ctx)
	if err != nil {
		return CommandError{
			Code:    CommandInternal,
			Message: "endpoint catalog could not be loaded",
			Err:     err,
		}
	}
	next := make([]endpointintent.Endpoint, 0, len(endpoints))
	removed := false
	for _, endpoint := range endpoints {
		if endpoint.Name() == parsed {
			removed = true
			continue
		}
		next = append(next, endpoint)
	}
	if !removed {
		return CommandError{
			Code:    CommandNotFound,
			Message: "endpoint could not be resolved",
			Err:     fs.ErrNotExist,
		}
	}
	if err := s.repo.SaveEndpoints(ctx, next); err != nil {
		return CommandError{
			Code:    CommandInternal,
			Message: "endpoint could not be deleted",
			Err:     err,
		}
	}
	return nil
}

func mapRepoError(err error) error {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return CommandError{
			Code:    CommandNotFound,
			Message: "endpoint could not be resolved",
			Err:     err,
		}
	default:
		return CommandError{
			Code:    CommandInternal,
			Message: "endpoint catalog could not be loaded",
			Err:     err,
		}
	}
}
