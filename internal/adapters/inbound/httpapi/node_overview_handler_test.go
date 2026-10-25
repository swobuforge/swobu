package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/swobuforge/swobu/internal/nodeauthority"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestNodeOverviewLocalOnlyRedactedAndRevisionBound(t *testing.T) {
	revision := strings.Repeat("a", 64)
	handler := NewNodeOverviewHandler(func(context.Context) (nodeauthority.Overview, error) {
		return nodeauthority.Overview{Revision: revision, ConfigurationMode: "externally_managed", Workspaces: 3, Connections: 2, Principals: 1, Publications: 2, BudgetPolicies: 1}, nil
	})
	direct := httptest.NewRecorder()
	handler.ServeHTTP(direct, httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/v1/node", nil))
	if direct.Code != http.StatusForbidden || direct.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("missing local gate", direct.Code)
	}
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/v1/node", nil)
	request = request.WithContext(contextWithAuthority(request, admittedAuthority(func(routing.WorkspaceSlug) (accessidentity.ID, error) { return accessidentity.Local, nil })))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 200 || response.Header().Get("ETag") != `"`+revision+`"` || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("invalid Node read", response.Code, response.Header())
	}
	var value map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	if len(value) != 7 || value["revision"] != revision || value["configuration_mode"] != "externally_managed" {
		t.Fatalf("unexpected disclosure %+v", value)
	}
	if _, has := value["credentials"]; has {
		t.Fatal("secret material disclosed")
	}
	credential := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/v1/node", nil)
	credential = credential.WithContext(contextWithAuthority(credential, admittedAuthority(func(routing.WorkspaceSlug) (accessidentity.ID, error) {
		return accessidentity.NewManaged([16]byte{2}), nil
	})))
	denied := httptest.NewRecorder()
	handler.ServeHTTP(denied, credential)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("Workspace-level authority entered Node read: %d", denied.Code)
	}
}

func TestNodeOverviewErrorsStaySecretFree(t *testing.T) {
	handler := NewNodeOverviewHandler(func(context.Context) (nodeauthority.Overview, error) {
		return nodeauthority.Overview{}, errors.New("SECRET=private_locator")
	})
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/v1/node", nil)
	request = request.WithContext(contextWithAuthority(request, admittedAuthority(func(routing.WorkspaceSlug) (accessidentity.ID, error) { return accessidentity.Local, nil })))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable || strings.Contains(response.Body.String(), "private_locator") || response.Header().Get("ETag") != "" {
		t.Fatal("Node leaked error or revision", response.Code, response.Body)
	}
}
