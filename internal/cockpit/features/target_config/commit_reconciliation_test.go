package target_config

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestCommitEditReplacesStateFromCommittedResponseAndPreservesItOnFailure(t *testing.T) {
	originalTarget := readmodel.TargetReadModel{ID: "primary", Model: "before", Provider: "openai", ProviderProtocol: "responses", CredentialRef: "env:OLD"}
	originalRoute := readmodel.RouteReadModel{ID: "chat", Tiers: []readmodel.TierReadModel{{Targets: []readmodel.TargetReadModel{originalTarget}}}}
	committedTarget := readmodel.TargetReadModel{ID: "primary", Model: "normalized", Provider: "openai", ProviderProtocol: "responses", CredentialRef: "env:COMMITTED"}
	committedRoute := readmodel.RouteReadModel{ID: "chat", Tiers: []readmodel.TierReadModel{{Targets: []readmodel.TargetReadModel{committedTarget}}}}

	saves := 0
	var savedConnection routing.Connection
	save := func(_ context.Context, request ports.SaveTargetRequest) (ports.SaveTargetResult, error) {
		saves++
		savedConnection = request.Connection
		if saves == 1 {
			return ports.SaveTargetResult{Target: committedTarget, Route: committedRoute}, nil
		}
		return ports.SaveTargetResult{}, errors.New("commit rejected")
	}
	config := NewEditTargetConfig("dev", originalRoute, originalTarget, save, nil)
	config.CommitEdit(context.Background())
	connection, ok := savedConnection.(routing.APIKeyConnection)
	if !ok || connection.Credential().String() != "env:OLD" {
		t.Fatalf("save connection = %#v; want typed OpenAI connection", savedConnection)
	}
	if !reflect.DeepEqual(config.Target, committedTarget) || !reflect.DeepEqual(config.Route, committedRoute) {
		t.Fatalf("state after success = target %#v route %#v; want committed response", config.Target, config.Route)
	}

	beforeTarget, beforeRoute := config.Target, config.Route
	config.CommitEdit(context.Background())
	if !reflect.DeepEqual(config.Target, beforeTarget) || !reflect.DeepEqual(config.Route, beforeRoute) {
		t.Fatalf("state changed after failed commit: target %#v route %#v", config.Target, config.Route)
	}
	if config.Error.Get() != "commit rejected" {
		t.Fatalf("failure error = %q", config.Error.Get())
	}
}
