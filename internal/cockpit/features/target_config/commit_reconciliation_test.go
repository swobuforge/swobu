package target_config

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	workspaceapi "github.com/swobuforge/swobu/internal/app/operator/workspaces"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestCommitEditReplacesStateFromCommittedResponseAndPreservesItOnFailure(t *testing.T) {
	const daemonCredential = "file:/home/operator/.config/provider.key"
	originalTarget := readmodel.TargetReadModel{ID: "primary", Model: "before", Provider: "openai", ProviderProtocol: "responses", CredentialRef: daemonCredential}
	originalRoute := readmodel.RouteReadModel{ID: "chat", Tiers: []readmodel.TierReadModel{{Targets: []readmodel.TargetReadModel{originalTarget}}}}
	committedTarget := readmodel.TargetReadModel{ID: "primary", Model: "normalized", Provider: "openai", ProviderProtocol: "responses", CredentialRef: "env:COMMITTED"}
	committedRoute := readmodel.RouteReadModel{ID: "chat", Tiers: []readmodel.TierReadModel{{Targets: []readmodel.TargetReadModel{committedTarget}}}}

	saves := 0
	var savedConnection routing.ConnectionDraft
	var serializedConnection []byte
	save := func(_ context.Context, request ports.SaveTargetRequest) (ports.SaveTargetResult, error) {
		saves++
		savedConnection = request.Connection
		serializedConnection, _ = json.Marshal(workspaceapi.ConnectionFromDraft(request.Connection))
		if saves == 1 {
			return ports.SaveTargetResult{Target: committedTarget, Route: committedRoute}, nil
		}
		return ports.SaveTargetResult{}, errors.New("commit rejected")
	}
	config := NewEditTargetConfig("dev", originalRoute, originalTarget, save, nil)
	config.CommitEdit(context.Background())
	if savedConnection.Standard == nil || savedConnection.Standard.Credential != daemonCredential {
		t.Fatalf("save connection = %#v; want unchanged daemon credential", savedConnection)
	}
	var wire map[string]struct {
		Credential string `json:"credential"`
	}
	if err := json.Unmarshal(serializedConnection, &wire); err != nil {
		t.Fatal(err)
	}
	if got := wire["openai"].Credential; got != daemonCredential {
		t.Fatalf("serialized credential = %q, want %q", got, daemonCredential)
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
