package runtime_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/metrofun/swobu/internal/domain/endpointintent"
	"github.com/metrofun/swobu/internal/domain/protocolsurface"
	tuieffect "github.com/metrofun/swobu/internal/terminalui/apps/cockpit/app/state/effect"
	stateModel "github.com/metrofun/swobu/internal/terminalui/apps/cockpit/app/state/model"
)

func TestTUIRoutingAliasMutation_PropagatesToCompatibilityModels(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[{"id":"m"}]}`))
	}))
	defer upstream.Close()

	daemon := startDaemon(t, runtimeFixture{
		endpoints: []endpointintent.Endpoint{
			testEndpoint(t, "alpha", "backend-a", testProviderConfig(t, "backend-a", "custom", upstream.URL+"/v1", "", protocolsurface.ChatCompletions)),
		},
	})
	defer func() { _ = daemon.Close(context.Background()) }()

	t.Setenv("SWOBU_DAEMON_URL", daemon.BaseURL())

	eff := tuieffect.SaveProviderConfigEffect{
		EndpointName: "alpha",
		ProviderConfig: stateModel.ProviderConfigSnapshot{
			Ref:          "backend-a",
			ProviderSpec: "custom",
			BaseURL:      upstream.URL + "/v1",
			ModelID:      "m",
			TargetAlias:  "fast",
			ProtocolKind: protocolsurface.ChatCompletions.String(),
		},
	}
	actions := eff.Execute(context.Background())
	if len(actions) != 1 {
		t.Fatalf("actions len = %d, want 1", len(actions))
	}
	if _, ok := actions[0].(tuieffect.RoutingMutationSaved); !ok {
		t.Fatalf("action[0] = %T, want tuieffect.RoutingMutationSaved", actions[0])
	}

	resp, err := http.Get(daemon.BaseURL() + "/c/alpha/v1/models")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	ids := map[string]bool{}
	for _, item := range body.Data {
		ids[item.ID] = true
	}
	for _, want := range []string{"fast"} {
		if !ids[want] {
			t.Fatalf("/models ids = %#v, want %q", ids, want)
		}
	}
	if ids["m"] {
		t.Fatalf("/models ids = %#v, aliased model must not also appear under mechanical id", ids)
	}
}
