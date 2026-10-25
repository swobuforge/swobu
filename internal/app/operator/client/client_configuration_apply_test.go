package operatorclient

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	connectapp "github.com/swobuforge/swobu/internal/app/operator/connect"
)

func connectIntentFixture(t *testing.T) (ClientConfigurationIntent, connectapp.Plan) {
	t.Helper()
	plan := connectapp.Plan{Authentication: connectapp.Authentication{Principal: "acc_AAAAAAAAAAAAAAAAAAAAAA", PermissionRevision: "permission-fixture", CredentialID: "acred_AAAAAAAAAAAAAAAAAAAAAA"}, Revision: "connect-" + strings.Repeat("a", 64), Workspace: "research", ModelID: "coding", Client: connectapp.Client{ID: "codex"}, ConfigPaths: []string{"fixture/config.toml"}, Changes: []connectapp.Change{{Field: "endpoint", Before: "private-original", BeforeExists: true, After: "new"}}, RequiresReplace: true}
	intent, err := NewClientConfigurationIntent(plan, "original-connect-key")
	if err != nil {
		t.Fatal(err)
	}
	return intent, plan
}

func TestClientConfigurationIntentFreezesOriginalReviewAndRecovery(t *testing.T) {
	intent, plan := connectIntentFixture(t)
	plan.ConfigPaths[0] = "changed"
	plan.Changes[0].Before = "changed"
	var originalBody string
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls++
		raw, _ := io.ReadAll(request.Body)
		if calls == 1 {
			originalBody = string(raw)
		}
		var body struct {
			Review connectapp.Review `json:"review"`
		}
		if json.Unmarshal(raw, &body) != nil || body.Review.ConfigPaths[0] != "fixture/config.toml" || body.Review.Changes[0].Before != "private-original" || string(raw) != originalBody || request.Method != http.MethodPost || request.URL.RequestURI() != "/api/v1/client-configurations?dry_run=false" || request.Header.Get("If-Match") != `"`+intent.revision+`"` || request.Header.Get("Idempotency-Key") != "original-connect-key" {
			t.Error("apply changed original intent")
		}
		writer.Header().Set("ETag", `"`+intent.revision+`"`)
		fmt.Fprintf(writer, `{"revision":%q,"workspace":"research","model_id":"coding","client":{"id":"codex"},"authentication":{"principal":"acc_AAAAAAAAAAAAAAAAAAAAAA","permission_revision":"permission-fixture","issue_credential":false,"credential_id":"acred_AAAAAAAAAAAAAAAAAAAAAA"},"config_paths":["fixture/config.toml"],"changes":[],"already_configured":true,"requires_replace":false,"replayed":%t}`, intent.revision, calls == 2)
	}))
	defer server.Close()
	client := New(server.Client(), server.URL)
	for _, replayed := range []bool{false, true} {
		result, err := client.ApplyClientConfiguration(t.Context(), intent)
		if err != nil || result.Replayed != replayed || !result.AlreadyConfigured {
			t.Fatal("original-intent acknowledgement lost")
		}
	}
	if calls != 2 {
		t.Fatal("unexpected dispatch")
	}
}

func TestClientConfigurationApplyRejectsMalformedAcknowledgementWithoutRetry(t *testing.T) {
	intent, _ := connectIntentFixture(t)
	valid := fmt.Sprintf(`{"revision":%q,"workspace":"research","model_id":"coding","client":{"id":"codex"},"authentication":{"principal":"acc_AAAAAAAAAAAAAAAAAAAAAA","permission_revision":"permission-fixture","issue_credential":false,"credential_id":"acred_AAAAAAAAAAAAAAAAAAAAAA"},"config_paths":["fixture/config.toml"],"changes":[],"already_configured":true,"requires_replace":false,"replayed":false}`, intent.revision)
	for _, body := range []string{
		strings.Replace(valid, `,"replayed":false`, "", 1),
		strings.Replace(valid, `"replayed":false`, `"replayed":null`, 1),
		strings.Replace(valid, `"already_configured":true`, `"already_configured":null`, 1),
		strings.Replace(valid, `"requires_replace":false`, `"requires_replace":true`, 1),
		strings.Replace(valid, `"research"`, `"other"`, 1),
		strings.Replace(valid, `"model_id":"coding"`, `"model_id":"other"`, 1),
		strings.Replace(valid, `"codex"`, `"claude"`, 1),
		strings.Replace(valid, `"fixture/config.toml"`, `"other"`, 1),
		strings.Replace(valid, `"changes":[]`, `"changes":null`, 1),
		strings.Replace(valid, `"already_configured":true`, `"already_configured":false`, 1),
		strings.Replace(valid, `,"requires_replace":false`, "", 1),
		strings.Replace(valid, `"requires_replace":false`, `"requires_replace":null`, 1),
		strings.Replace(valid, `"revision":"connect-`, `"revision":"wrong-`, 1),
		valid + ` {}`,
	} {
		t.Run(body, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				calls++
				writer.Header().Set("ETag", `"`+intent.revision+`"`)
				fmt.Fprint(writer, body)
			}))
			defer server.Close()
			result, err := New(server.Client(), server.URL).ApplyClientConfiguration(t.Context(), intent)
			if err == nil || calls != 1 || result.Client.ID != "" {
				t.Fatal("invalid acknowledgement accepted or retried")
			}
		})
	}
}

func TestClientConfigurationApplyRejectsChangedETagAndNodeDenial(t *testing.T) {
	intent, _ := connectIntentFixture(t)
	for _, status := range []int{http.StatusOK, http.StatusForbidden, http.StatusConflict} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				calls++
				writer.Header().Set("ETag", `"changed"`)
				writer.WriteHeader(status)
				fmt.Fprintf(writer, `{"revision":%q,"workspace":"research","model_id":"coding","client":{"id":"codex"},"authentication":{"principal":"acc_AAAAAAAAAAAAAAAAAAAAAA","permission_revision":"permission-fixture","issue_credential":false,"credential_id":"acred_AAAAAAAAAAAAAAAAAAAAAA"},"config_paths":["fixture/config.toml"],"changes":[],"already_configured":true,"requires_replace":false,"replayed":false}`, intent.revision)
			}))
			defer server.Close()
			result, err := New(server.Client(), server.URL).ApplyClientConfiguration(t.Context(), intent)
			if err == nil || calls != 1 || result.Client.ID != "" {
				t.Fatal("changed ETag or Node denial accepted or retried")
			}
		})
	}
}

type connectUncertainTransport struct {
	calls   int
	testing *testing.T
}

func (transport *connectUncertainTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.calls++
	if request.GetBody != nil {
		transport.testing.Error("transport body replay enabled")
	}
	return nil, fmt.Errorf("private transport detail")
}

func TestClientConfigurationUncertaintyAndInvalidIntentDoNotRetry(t *testing.T) {
	intent, plan := connectIntentFixture(t)
	transport := &connectUncertainTransport{testing: t}
	client := New(&http.Client{Transport: transport}, "http://127.0.0.1:1234")
	if _, err := client.ApplyClientConfiguration(t.Context(), ClientConfigurationIntent{}); err == nil || transport.calls != 0 {
		t.Fatal("empty intent reached Node")
	}
	if _, err := client.ApplyClientConfiguration(t.Context(), intent); err == nil || strings.Contains(err.Error(), "private transport detail") || transport.calls != 1 {
		t.Fatal("uncertainty retried or disclosed transport data")
	}
	plan.Revision = "invalid"
	if _, err := NewClientConfigurationIntent(plan, "original-connect-key"); err == nil {
		t.Fatal("invalid revision retained")
	}
}

func TestClientConfigurationIntentRejectsIncompleteReview(t *testing.T) {
	for _, mutate := range []func(*connectapp.Plan){
		func(plan *connectapp.Plan) { plan.Workspace = "Research" },
		func(plan *connectapp.Plan) { plan.Client.ID = "unsupported" },
		func(plan *connectapp.Plan) { plan.ConfigPaths = nil },
		func(plan *connectapp.Plan) { plan.Changes = nil },
		func(plan *connectapp.Plan) { plan.Changes[0].Field = "" },
		func(plan *connectapp.Plan) { plan.AlreadyConfigured = true },
		func(plan *connectapp.Plan) { plan.RequiresReplace = false },
	} {
		_, plan := connectIntentFixture(t)
		mutate(&plan)
		if _, err := NewClientConfigurationIntent(plan, "original-connect-key"); err == nil {
			t.Fatal("incomplete review retained")
		}
	}
	_, plan := connectIntentFixture(t)
	for _, key := range []string{"", "short", strings.Repeat("a", 257), "original-connect\r\nkey"} {
		if _, err := NewClientConfigurationIntent(plan, key); err == nil {
			t.Fatal("invalid retry key retained")
		}
	}
}
