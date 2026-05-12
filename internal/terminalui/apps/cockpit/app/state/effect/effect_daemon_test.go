package effect

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNormalizeOperatorSurfaceError_MapsTransportUnavailabilityToOperatorHint(t *testing.T) {
	t.Setenv("SWOBU_DAEMON_URL", "http://127.0.0.1:8787")

	got := normalizeOperatorSurfaceError(errors.New("operator client: endpoint list is unavailable"))
	want := "unavailable at http://127.0.0.1:8787"
	if got != want {
		t.Fatalf("normalizeOperatorSurfaceError = %q, want %q", got, want)
	}
}

func TestNormalizeOperatorSurfaceError_PreservesNonTransportMessageWithoutInternalPrefix(t *testing.T) {
	got := normalizeOperatorSurfaceError(errors.New("operator client: endpoint decode failed: invalid json"))
	if !strings.Contains(got, "endpoint decode failed") {
		t.Fatalf("normalizeOperatorSurfaceError = %q, want decode failure detail", got)
	}
	if strings.Contains(got, "operator client:") {
		t.Fatalf("normalizeOperatorSurfaceError = %q, want internal prefix removed", got)
	}
}

func TestLoadJSON_ModelProbe404_HasStaleDaemonHint(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	_, err := loadJSON[map[string]any](context.Background(), srv.URL+"/_swobu/model-catalog/probe?provider_spec=openai")
	if err == nil {
		t.Fatal("loadJSON returned nil error, want stale-daemon hint")
	}
	if !strings.Contains(err.Error(), "/_swobu/model-catalog/probe") {
		t.Fatalf("error = %q, want probe route hint", err.Error())
	}
}

func TestLoadJSON_AllowsUnknownFields(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"extra":"unexpected"}`))
	}))
	defer srv.Close()

	type payload struct {
		OK bool `json:"ok"`
	}
	got, err := loadJSON[payload](context.Background(), srv.URL+"/_swobu/status")
	if err != nil {
		t.Fatalf("loadJSON returned error: %v", err)
	}
	if !got.OK {
		t.Fatalf("payload.OK = %v, want true", got.OK)
	}
}

func TestLoadRoutingModelCatalogEffect_SlowProbeMapsToTimeoutHint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_swobu/model-catalog/probe" {
			http.NotFound(w, r)
			return
		}
		time.Sleep(500 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model_ids":["openrouter/m1"]}`))
	}))
	defer srv.Close()

	t.Setenv("SWOBU_DAEMON_URL", srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	actions := (LoadRoutingModelCatalogEffect{
		Scope:         "add_model_draft",
		ProviderSpec:  "openrouter",
		BaseURL:       "https://openrouter.ai/api/v1",
		CredentialRef: "file:/tmp/openrouter.key",
		ProtocolKind:  "chat_completions",
	}).Execute(ctx)

	if len(actions) != 1 {
		t.Fatalf("actions length = %d, want 1", len(actions))
	}
	loaded, ok := actions[0].(RoutingModelCatalogLoaded)
	if !ok {
		t.Fatalf("action type = %T, want RoutingModelCatalogLoaded", actions[0])
	}
	want := "model probe timed out at " + srv.URL + " (retry)"
	if loaded.Error != want {
		t.Fatalf("error = %q, want %q", loaded.Error, want)
	}
	if len(loaded.ModelIDs) != 0 {
		t.Fatalf("model ids = %#v, want empty on timeout", loaded.ModelIDs)
	}
}

func TestRefreshStatusProjectionEffect_MissingObservedAt_FailsFast(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_swobu/status-projection" {
			http.NotFound(w, r)
			return
		}
		if got := r.URL.Query().Get("scope"); got != "all" {
			http.Error(w, "missing expected scope", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"scope":{"kind":"all"},"recent_traffic":[{"request_id":"req_1","route":"primary","result":"backend_error","status_code":400,"timing":{"dur_millis":0}}]}`))
	}))
	defer srv.Close()

	t.Setenv("SWOBU_DAEMON_URL", srv.URL)
	actions := (RefreshStatusProjectionEffect{}).Execute(context.Background())
	if len(actions) != 1 {
		t.Fatalf("actions length = %d, want 1", len(actions))
	}
	failed, ok := actions[0].(TrafficLoadFailed)
	if !ok {
		t.Fatalf("action type = %T, want TrafficLoadFailed", actions[0])
	}
	if !strings.Contains(strings.ToLower(failed.Message), "missing observed_at") {
		t.Fatalf("failure message = %q, want missing observed_at", failed.Message)
	}
}

func TestRefreshStatusProjectionEffect_EndpointScopeMismatch_FailsFast(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_swobu/status-projection" {
			http.NotFound(w, r)
			return
		}
		if got := r.URL.Query().Get("scope"); got != "endpoint:acme" {
			http.Error(w, "missing expected endpoint scope", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"scope":{"kind":"endpoint","endpoint":"staging"},"recent_traffic":[]}`))
	}))
	defer srv.Close()

	t.Setenv("SWOBU_DAEMON_URL", srv.URL)
	actions := (RefreshStatusProjectionEffect{EndpointName: "acme"}).Execute(context.Background())
	if len(actions) != 1 {
		t.Fatalf("actions length = %d, want 1", len(actions))
	}
	failed, ok := actions[0].(TrafficLoadFailed)
	if !ok {
		t.Fatalf("action type = %T, want TrafficLoadFailed", actions[0])
	}
	if !strings.Contains(strings.ToLower(failed.Message), "scope endpoint mismatch") {
		t.Fatalf("failure message = %q, want scope endpoint mismatch", failed.Message)
	}
}

func TestRefreshStatusProjectionEffect_MapsTokenAndCacheUsageFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_swobu/status-projection" {
			http.NotFound(w, r)
			return
		}
		if got := r.URL.Query().Get("scope"); got != "all" {
			http.Error(w, "missing expected scope", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"scope":{"kind":"all"},"recent_traffic":[{"request_id":"req_1","route":"primary","ingress_family":"responses","result":"success","status_code":200,"observed_at":"12:00:00","token_usage":{"input_tokens":120,"output_tokens":9,"cache_read_tokens":70,"cache_write_tokens":5}}]}`))
	}))
	defer srv.Close()

	t.Setenv("SWOBU_DAEMON_URL", srv.URL)
	actions := (RefreshStatusProjectionEffect{}).Execute(context.Background())
	if len(actions) != 1 {
		t.Fatalf("actions length = %d, want 1", len(actions))
	}
	replaced, ok := actions[0].(ReplaceStatusProjection)
	if !ok {
		t.Fatalf("action type = %T, want ReplaceStatusProjection", actions[0])
	}
	if len(replaced.Rows) != 1 {
		t.Fatalf("rows length = %d, want 1", len(replaced.Rows))
	}
	row := replaced.Rows[0]
	if row.InputTokens == nil || *row.InputTokens != 120 {
		t.Fatalf("row.InputTokens = %#v, want 120", row.InputTokens)
	}
	if row.OutputTokens == nil || *row.OutputTokens != 9 {
		t.Fatalf("row.OutputTokens = %#v, want 9", row.OutputTokens)
	}
	if row.CacheReadTokens == nil || *row.CacheReadTokens != 70 {
		t.Fatalf("row.CacheReadTokens = %#v, want 70", row.CacheReadTokens)
	}
	if row.CacheWriteTokens == nil || *row.CacheWriteTokens != 5 {
		t.Fatalf("row.CacheWriteTokens = %#v, want 5", row.CacheWriteTokens)
	}
}

func TestRefreshDaemonStatusEffect_MissingControlPlaneProtocolFailsFast(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_swobu/status" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"state":"healthy","endpoint_count":1,"swobu_version":"0.9.0"}`))
	}))
	defer srv.Close()

	t.Setenv("SWOBU_DAEMON_URL", srv.URL)
	actions := (RefreshDaemonStatusEffect{}).Execute(context.Background())
	if len(actions) != 1 {
		t.Fatalf("actions length = %d, want 1", len(actions))
	}
	incompatible, ok := actions[0].(ControlPlaneIncompatibleDetected)
	if !ok {
		t.Fatalf("action type = %T, want ControlPlaneIncompatibleDetected", actions[0])
	}
	if incompatible.HasDaemonProtocol {
		t.Fatal("HasDaemonProtocol = true, want false")
	}
}

func TestRefreshDaemonStatusEffect_ProtocolMismatchFailsFast(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_swobu/status" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"state":"healthy","endpoint_count":1,"control_plane_protocol":6,"swobu_version":"0.8.4"}`))
	}))
	defer srv.Close()

	t.Setenv("SWOBU_DAEMON_URL", srv.URL)
	actions := (RefreshDaemonStatusEffect{}).Execute(context.Background())
	if len(actions) != 1 {
		t.Fatalf("actions length = %d, want 1", len(actions))
	}
	incompatible, ok := actions[0].(ControlPlaneIncompatibleDetected)
	if !ok {
		t.Fatalf("action type = %T, want ControlPlaneIncompatibleDetected", actions[0])
	}
	if !incompatible.HasDaemonProtocol {
		t.Fatal("HasDaemonProtocol = false, want true")
	}
	if incompatible.DaemonProtocol != 6 {
		t.Fatalf("DaemonProtocol = %d, want 6", incompatible.DaemonProtocol)
	}
	if incompatible.DaemonVersion != "0.8.4" {
		t.Fatalf("DaemonVersion = %q, want %q", incompatible.DaemonVersion, "0.8.4")
	}
}

func TestRefreshDaemonStatusEffect_MissingSwobuVersionFailsFast(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_swobu/status" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"state":"healthy","endpoint_count":1,"control_plane_protocol":7}`))
	}))
	defer srv.Close()

	t.Setenv("SWOBU_DAEMON_URL", srv.URL)
	actions := (RefreshDaemonStatusEffect{}).Execute(context.Background())
	if len(actions) != 1 {
		t.Fatalf("actions length = %d, want 1", len(actions))
	}
	incompatible, ok := actions[0].(ControlPlaneIncompatibleDetected)
	if !ok {
		t.Fatalf("action type = %T, want ControlPlaneIncompatibleDetected", actions[0])
	}
	if !strings.Contains(strings.ToLower(incompatible.Reason), "missing required swobu_version") {
		t.Fatalf("reason = %q, want missing swobu_version", incompatible.Reason)
	}
}
