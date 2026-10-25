package bootstrap

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/swobuforge/swobu/internal/platform/config"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestWorkspaceErrorProjectionDistinguishesMissingUnconfiguredAndUnavailable(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "swobu.yaml")
	var providerRequests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		providerRequests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, `{"error":{"message":"upstream unavailable"}}`)
	}))
	defer upstream.Close()
	writeCurrentWorkspaceFixture(t, configPath, root, "unavailable", upstream.URL, "responses", false)
	current, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	current = bytes.Replace(current, []byte("workspaces:\n"), []byte("workspaces:\n  empty:\n    routes: []\n"), 1)
	if err := os.WriteFile(configPath, current, 0o600); err != nil {
		t.Fatal(err)
	}
	daemon, err := Start(t.Context(), StartInput{ConfigPath: configPath, StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"}})
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	missingModels, err := http.Get(daemon.BaseURL() + "/workspaces/missing/v1/models")
	if err != nil {
		t.Fatal(err)
	}
	missingModelsBody, err := io.ReadAll(missingModels.Body)
	missingModels.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if missingModels.StatusCode != http.StatusNotFound {
		t.Fatalf("missing Workspace models status=%d body=%s", missingModels.StatusCode, missingModelsBody)
	}
	var missingModelsEnvelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(missingModelsBody, &missingModelsEnvelope); err != nil || missingModelsEnvelope.Error.Code != "BAD_ENDPOINT" {
		t.Fatalf("missing Workspace models body=%s code=%q err=%v", missingModelsBody, missingModelsEnvelope.Error.Code, err)
	}
	emptySlug, _ := routing.ParseWorkspaceSlug("empty")
	_, emptyCredential, err := daemon.accessStore.Create("empty-matrix", emptySlug)
	if err != nil {
		t.Fatal(err)
	}
	unavailableSlug, _ := routing.ParseWorkspaceSlug("unavailable")
	_, unavailableCredential, err := daemon.accessStore.Create("unavailable-matrix", unavailableSlug)
	if err != nil {
		t.Fatal(err)
	}
	modelsRequest, err := http.NewRequest(http.MethodGet, daemon.BaseURL()+"/workspaces/unavailable/v1/models", nil)
	if err != nil {
		t.Fatal(err)
	}
	modelsRequest.Header.Set("Authorization", "Bearer "+unavailableCredential.Token)
	modelsResponse, err := http.DefaultClient.Do(modelsRequest)
	if err != nil {
		t.Fatal(err)
	}
	modelsBody, err := io.ReadAll(modelsResponse.Body)
	modelsResponse.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	var models struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if modelsResponse.StatusCode != http.StatusOK || json.Unmarshal(modelsBody, &models) != nil || len(models.Data) != 1 || models.Data[0].ID != "chat" {
		t.Fatalf("configured Workspace models status=%d body=%s", modelsResponse.StatusCode, modelsBody)
	}
	if providerRequests.Load() != 0 {
		t.Fatal("model discovery contacted the provider")
	}

	cases := []struct {
		name       string
		path       string
		bearer     string
		wantStatus int
		wantCode   string
		wantBody   string
	}{
		{name: "missing workspace", path: "/workspaces/missing/v1/responses", bearer: "", wantStatus: http.StatusNotFound, wantCode: "BAD_ENDPOINT"},
		{name: "operation not configured", path: "/workspaces/empty/v1/responses", bearer: emptyCredential.Token, wantStatus: http.StatusBadRequest, wantCode: "OPERATION_NOT_CONFIGURED"},
		{name: "provider unavailable", path: "/workspaces/unavailable/v1/responses", bearer: unavailableCredential.Token, wantStatus: http.StatusServiceUnavailable, wantBody: "upstream unavailable"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			request, err := http.NewRequest(http.MethodPost, daemon.BaseURL()+testCase.path, strings.NewReader(`{"model":"chat","input":"hello"}`))
			if err != nil {
				t.Fatal(err)
			}
			if testCase.bearer != "" {
				request.Header.Set("Authorization", "Bearer "+testCase.bearer)
			}
			request.Header.Set("Content-Type", "application/json")
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != testCase.wantStatus {
				t.Fatalf("status=%d want=%d body=%s", response.StatusCode, testCase.wantStatus, body)
			}
			if testCase.wantBody != "" {
				if !strings.Contains(string(body), testCase.wantBody) {
					t.Fatalf("body=%s want upstream marker %q", body, testCase.wantBody)
				}
				return
			}
			var envelope struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(body, &envelope); err != nil || envelope.Error.Code != testCase.wantCode {
				t.Fatalf("body=%s code=%q want=%q err=%v", body, envelope.Error.Code, testCase.wantCode, err)
			}
		})
	}
	if providerRequests.Load() != 1 {
		t.Fatalf("configured route should dispatch once; provider requests=%d", providerRequests.Load())
	}
}
