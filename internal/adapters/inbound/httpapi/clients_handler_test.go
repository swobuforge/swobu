package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	connectapp "github.com/swobuforge/swobu/internal/app/operator/connect"
	"github.com/swobuforge/swobu/internal/clientconnect"
	"github.com/swobuforge/swobu/internal/routing"
)

type discoveryOnlyClients struct {
	discovered int
	target     clientconnect.Target
	cancel     context.CancelFunc
}

func (clients *discoveryOnlyClients) Discover(_ context.Context, target clientconnect.Target) []clientconnect.Client {
	clients.discovered++
	clients.target = target
	if clients.cancel != nil {
		clients.cancel()
	}
	return []clientconnect.Client{{ID: "codex", Name: "Codex"}}
}

func (*discoveryOnlyClients) Plan(context.Context, clientconnect.ClientID, clientconnect.Target) (clientconnect.Plan, error) {
	panic("discovery must not plan or read configuration")
}

func (*discoveryOnlyClients) Apply(context.Context, clientconnect.Plan) (clientconnect.Plan, error) {
	panic("discovery must not mutate configuration")
}

func TestClientsHandlerClosedLocalPresenceDiscovery(t *testing.T) {
	clients := &discoveryOnlyClients{}
	service, err := connectapp.NewService(clients, connectTestCredentials{})
	if err != nil {
		t.Fatal(err)
	}
	lookups := 0
	handler := NewLocalAdmissionHandler(NewClientsHandler(service, "http://127.0.0.1:7926", func(_ *http.Request, slug routing.WorkspaceSlug) bool {
		lookups++
		return slug.String() == "research"
	}), nil, nil)
	server := httptest.NewServer(handler)
	defer server.Close()
	response, err := server.Client().Get(server.URL + "/api/v1/clients?workspace=research")
	if err != nil {
		t.Fatal(err)
	}
	var result []connectapp.Client
	err = json.NewDecoder(response.Body).Decode(&result)
	response.Body.Close()
	if err != nil || response.StatusCode != http.StatusOK || response.Header.Get("Cache-Control") != "no-store" || len(result) != 1 || result[0].ID != "codex" || clients.discovered != 1 || clients.target.WorkspaceURL() != "http://127.0.0.1:7926/workspaces/research" {
		t.Fatal("discovery lost canonical target or metadata")
	}
	for _, query := range []string{"", "workspace=Research", "workspace=research&workspace=other", "workspace=research&admin=true", "workspace=%zz", "workspace=research;admin=true"} {
		before := lookups
		request := httptest.NewRequest(http.MethodGet, "/api/v1/clients?"+query, nil)
		request.RemoteAddr = "127.0.0.1:1234"
		request.Host = "127.0.0.1:7926"
		writer := httptest.NewRecorder()
		handler.ServeHTTP(writer, request)
		if writer.Code != http.StatusBadRequest || lookups != before || clients.discovered != 1 {
			t.Fatalf("invalid discovery query reached owner: %s (%d)", query, writer.Code)
		}
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/clients?workspace=research", nil)
	writer := httptest.NewRecorder()
	NewClientsHandler(service, "http://127.0.0.1:7926", nil).ServeHTTP(writer, request)
	if writer.Code != http.StatusForbidden || clients.discovered != 1 {
		t.Fatal("unadmitted request reached client discovery")
	}
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		request := httptest.NewRequest(method, "/api/v1/clients?workspace=missing", nil)
		request.RemoteAddr = "127.0.0.1:1234"
		request.Host = "127.0.0.1:7926"
		writer := httptest.NewRecorder()
		handler.ServeHTTP(writer, request)
		want := http.StatusNotFound
		if method == http.MethodPost {
			want = http.StatusMethodNotAllowed
		}
		if writer.Code != want || clients.discovered != 1 {
			t.Fatal("missing Workspace or mutator reached discovery")
		}
	}
}

func TestClientsHandlerNeverPublishesCanceledInventory(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	clients := &discoveryOnlyClients{cancel: cancel}
	service, err := connectapp.NewService(clients, connectTestCredentials{})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewLocalAdmissionHandler(NewClientsHandler(service, "http://127.0.0.1:7926", func(*http.Request, routing.WorkspaceSlug) bool { return true }), nil, nil)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/clients?workspace=research", nil).WithContext(ctx)
	request.RemoteAddr = "127.0.0.1:1234"
	request.Host = "127.0.0.1:7926"
	writer := httptest.NewRecorder()
	handler.ServeHTTP(writer, request)
	if writer.Code != http.StatusServiceUnavailable || clients.discovered != 1 || strings.Contains(writer.Body.String(), "Codex") {
		t.Fatal("canceled discovery published partial inventory")
	}
}
