package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCredentialStoreHandlerPersistsInDaemonAndReturnsOnlyReference(t *testing.T) {
	var provider, name, secret string
	handler := NewCredentialStoreHandler(func(_ context.Context, gotProvider, gotName, gotSecret string) (string, error) {
		provider, name, secret = gotProvider, gotName, gotSecret
		return "secretfile:" + gotName, nil
	})
	request := httptest.NewRequest(http.MethodPost, "/_swobu/credentials", strings.NewReader(
		`{"provider_spec":"zai","name":"cockpit/target/personal/zai/target","secret":"zai-secret"}`,
	))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if provider != "zai" || name != "cockpit/target/personal/zai/target" || secret != "zai-secret" {
		t.Fatalf("stored provider=%q name=%q secret=%q", provider, name, secret)
	}
	if strings.Contains(response.Body.String(), "zai-secret") {
		t.Fatalf("response exposed secret material: %s", response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"credential_ref":"secretfile:cockpit/target/personal/zai/target"`) {
		t.Fatalf("response=%s", response.Body.String())
	}
}

func TestCredentialStoreHandlerRejectsIncompleteCommandBeforePersistence(t *testing.T) {
	called := false
	handler := NewCredentialStoreHandler(func(context.Context, string, string, string) (string, error) {
		called = true
		return "", nil
	})
	request := httptest.NewRequest(http.MethodPost, "/_swobu/credentials", strings.NewReader(
		`{"provider_spec":"custom","name":"slot","secret":""}`,
	))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest || called {
		t.Fatalf("status=%d called=%v body=%s", response.Code, called, response.Body.String())
	}
}
