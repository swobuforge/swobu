package operatorclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	workspaceapi "github.com/swobuforge/swobu/internal/app/operator/workspaces"
)

func TestModelRouteWriteUsesWorkspaceRevisionWithoutReplayIdentity(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodPut || r.URL.EscapedPath() != "/api/v1/workspaces/engineering/model-routes/deepseek%2F%2A" {
			t.Errorf("unexpected operation: %s %s", r.Method, r.URL.EscapedPath())
		}
		if r.Header.Get("If-Match") != `"lifetime-7"` || r.Header.Get("Idempotency-Key") != "" {
			t.Errorf("unexpected mutation headers: %v", r.Header)
		}
		var spec workspaceapi.RouteSpec
		if err := json.NewDecoder(r.Body).Decode(&spec); err != nil || len(spec.Tiers) != 1 || len(spec.Tiers[0].Targets) != 1 || spec.Tiers[0].Targets[0].Model != "deepseek-v4" {
			t.Errorf("unexpected route payload: %#v (%v)", spec, err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"slug":"engineering","management_etag":"lifetime-8"}`))
	}))
	defer server.Close()
	api := New(server.Client(), server.URL)
	command := workspaceapi.ReplaceRoute{Workspace: "engineering", Route: "deepseek/*", Spec: workspaceapi.RouteSpec{Tiers: []workspaceapi.TierSpec{{Targets: []workspaceapi.TargetDraft{{ID: "main", Model: "deepseek-v4", Protocol: "responses", ConnectionID: "cn_deepseek"}}}}}}
	committed, err := api.PutModelRoute(context.Background(), command, "lifetime-7")
	if err != nil || committed.Slug != "engineering" || committed.ManagementETag != "lifetime-8" || requests != 1 {
		t.Fatalf("response=%#v requests=%d err=%v", committed, requests, err)
	}
	if _, err := api.PutModelRoute(context.Background(), command, ""); err == nil || !strings.Contains(err.Error(), "ETag") || requests != 1 {
		t.Fatalf("missing revision must reject locally: requests=%d err=%v", requests, err)
	}
}
