package bootstrap

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hashicorp/yamux"
	"github.com/swobuforge/swobu/internal/accessstate"
	"github.com/swobuforge/swobu/internal/accounting"
	"github.com/swobuforge/swobu/internal/connections"
	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
	"github.com/swobuforge/swobu/internal/platform/config"
	"github.com/swobuforge/swobu/internal/relaytransport"
	"github.com/swobuforge/swobu/internal/routing"
)

// Exercise the assembled Owner handler over encrypted yamux streams. The relay
// only forwards bytes; Access, publication and Search remain Node authorities.
// Certificate issuance and hosted relay policy are separate qualification lanes.
func TestPublicSearchThroughOwnerRelaySurvivesRestartAndLivePolicyChanges(t *testing.T) {
	t.Setenv("SWOBU_RELAY_ADDR", "127.0.0.1:1")
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "swobu.yaml")
	if err := os.WriteFile(configPath, []byte(`schema_version: 5
workspaces:
  research:
    routes: []
    search:
      connection_id: cn_fixture_search
`), 0600); err != nil {
		t.Fatal(err)
	}
	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"results":[{"url":"https://docs.example.test","title":"Docs","content":"relay result"}]}`)
	}))
	t.Cleanup(upstream.Close)
	id, err := connectionidentity.Parse("cn_fixture_search")
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := connections.ParseEndpoint(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := connections.NewSearchSpec("fixture", endpoint, credentialref.Ref{})
	if err != nil {
		t.Fatal(err)
	}
	connection, err := connections.New(id, "Relay Search fixture", spec, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	publishCurrentConnectionFixtures(t, configPath, connection)
	input := StartInput{ConfigPath: configPath, StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"}}
	daemon, err := Start(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = daemon.Close() })
	workspace, err := routing.ParseWorkspaceSlug("research")
	if err != nil {
		t.Fatal(err)
	}
	setPublished := func(enabled bool) {
		t.Helper()
		unlock := daemon.publicationStore.LockLifecycle()
		defer unlock()
		if err := daemon.publicationStore.Set(workspace, enabled, daemon.publicationStore.Revision(), func() error { return nil }); err != nil {
			t.Fatal(err)
		}
	}
	setPublished(true)
	principal, credential, err := daemon.accessStore.Create("relay-search-client", workspace)
	if err != nil {
		t.Fatal(err)
	}
	setPermission := func(key string, services []routing.ServiceKind, rates []accessstate.RequestRateLimit) {
		t.Helper()
		revision, err := daemon.accessStore.PermissionRevision(principal.ID, workspace)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := daemon.accessStore.ApplyPermissionServicesWithRatesChecked(t.Context(), principal.ID, workspace, revision, key, func() error { return nil }, services, rates); err != nil {
			t.Fatal(err)
		}
	}
	setPermission("search-only", []routing.ServiceKind{routing.ServiceSearch}, nil)
	if err := daemon.Close(); err != nil {
		t.Fatal(err)
	}
	daemon, err = Start(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	if !daemon.publicationStore.Has(workspace) {
		t.Fatal("restart lost publication")
	}

	client := ownerRelaySearchClient(t, daemon.publicHandler)
	send := func(method, path, token string, want int) {
		t.Helper()
		request, err := http.NewRequestWithContext(t.Context(), method, "https://owner.example"+path, strings.NewReader(`{"query":"relay","limit":1}`))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/json")
		if token != "" {
			request.Header.Set("Authorization", "Bearer "+token)
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != want {
			t.Fatalf("%s %s: status=%d want=%d body=%s", method, path, response.StatusCode, want, body)
		}
		if want == http.StatusOK && !strings.Contains(string(body), "https://docs.example.test") {
			t.Fatalf("Search result missing: %s", body)
		}
	}
	send(http.MethodPost, "/workspaces/research/search", credential.Token, http.StatusOK)
	send(http.MethodPost, "/workspaces/research/search", "", http.StatusUnauthorized)
	send(http.MethodPost, "/workspaces/research/search", "swobu", http.StatusUnauthorized)
	for _, path := range []string{"/workspaces/other/search", "/workspaces/research/search/extra"} {
		send(http.MethodPost, path, credential.Token, http.StatusNotFound)
	}
	for _, path := range []string{"/api/v1/status", "/api/v1/workspaces", "/app/", "/workspaces/research/api/v1/status", "/workspaces/research/v1/files", "/workspaces/research/files/upload"} {
		send(http.MethodPost, path, credential.Token, http.StatusNotFound)
	}
	for _, path := range []string{"/workspaces/research/v1/files/private", "/workspaces/research/files/download", "/workspaces/research/../../api/v1/status"} {
		send(http.MethodGet, path, credential.Token, http.StatusNotFound)
	}
	send(http.MethodPost, "/workspaces/research/mcp", credential.Token, http.StatusForbidden)
	send(http.MethodGet, "/workspaces/research/v1/models", credential.Token, http.StatusForbidden)
	setPermission("revoke-search", []routing.ServiceKind{routing.ServiceModels}, nil)
	send(http.MethodPost, "/workspaces/research/search", credential.Token, http.StatusForbidden)
	setPermission("limit-search", []routing.ServiceKind{routing.ServiceSearch}, []accessstate.RequestRateLimit{{Service: routing.ServiceSearch, RequestsPerMinute: 1}})
	send(http.MethodPost, "/workspaces/research/search", credential.Token, http.StatusOK)
	send(http.MethodPost, "/workspaces/research/search", credential.Token, http.StatusTooManyRequests)
	setPublished(false)
	send(http.MethodPost, "/workspaces/research/search", credential.Token, http.StatusNotFound)
	setPublished(true)
	send(http.MethodPost, "/workspaces/research/search", "", http.StatusUnauthorized)
	if _, err := daemon.accessStore.RevokeCredential(principal.ID, credential.Credential.ID); err != nil {
		t.Fatal(err)
	}
	send(http.MethodPost, "/workspaces/research/search", credential.Token, http.StatusUnauthorized)
	_, budgetCredential, err := daemon.accessStore.Create("budget-search-client", workspace)
	if err != nil {
		t.Fatal(err)
	}
	// Public reachability never bypasses the Workspace's budget admission.
	before := upstreamCalls.Load()
	if err := daemon.accountingStore.SetLimit("research", accounting.Limit{Currency: "USD", AmountCents: 100, Window: "day", Mode: accounting.ModeStopNewBillableWork, UnknownPrice: "reject"}); err != nil {
		t.Fatal(err)
	}
	send(http.MethodPost, "/workspaces/research/search", budgetCredential.Token, http.StatusPaymentRequired)
	if upstreamCalls.Load() != before {
		t.Fatal("budget rejection reached the Search Connection")
	}
}

func ownerRelaySearchClient(t *testing.T, handler http.Handler) *http.Client {
	t.Helper()
	// Reuse httptest's certificate/trust setup, but send requests through the real
	// Owner stream/TLS implementation rather than its direct HTTPS listener.
	certificateFixture := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(certificateFixture.Close)
	relayConn, ownerConn := net.Pipe()
	relay, err := yamux.Server(relayConn, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	ownerDone := make(chan error, 1)
	go func() {
		ownerDone <- relaytransport.ServeOwner(ctx, ownerConn, nil, nil, nil, func(stream net.Conn) {
			_ = relaytransport.ServeApplicationTLS(stream, certificateFixture.TLS, handler)
		})
	}()
	control, err := relay.AcceptStream()
	if err != nil {
		t.Fatal(err)
	}
	transport := certificateFixture.Client().Transport.(*http.Transport).Clone()
	// Each simulated public connection owns one fresh forwarded stream.
	transport.DisableKeepAlives = true
	transport.DialContext = func(context.Context, string, string) (net.Conn, error) { return relay.OpenStream() }
	transport.TLSClientConfig.ServerName = "example.com"
	t.Cleanup(func() {
		transport.CloseIdleConnections()
		_ = control.Close()
		cancel()
		_ = relay.Close()
		_ = ownerConn.Close()
		select {
		case <-ownerDone:
		case <-time.After(5 * time.Second):
			t.Error("Owner relay did not stop")
		}
	})
	return &http.Client{Transport: transport, Timeout: 10 * time.Second}
}
