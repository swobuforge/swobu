package search

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/connections"
	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
)

func searchCheckConnection(t *testing.T, endpoint, ref string) connections.Connection {
	t.Helper()
	id, err := connectionidentity.New()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := connections.ParseEndpoint(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := connections.NewSearchSpec("qualified-json", parsed, credentialref.Parse(ref))
	if err != nil {
		t.Fatal(err)
	}
	value, err := connections.New(id, "Search", spec, time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestConnectionSearchCheckQualifiedResponseAndExactCredentialScope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Query().Get("q") != "swobu connection check" || r.URL.Query().Get("number_of_results") != "1" || r.URL.Query().Get("format") != "json" || r.Header.Get("Authorization") != "Bearer synthetic-token" {
			t.Error("wrong synthetic query or authorization")
		}
		w.Write([]byte(`{"results":[{"url":"https://example.test","title":"Synthetic"}]}`))
	}))
	defer server.Close()
	value := searchCheckConnection(t, server.URL, "env:SYNTHETIC")
	resolved := 0
	check, err := checkConnection(t.Context(), value, func(ctx context.Context, scope credentialref.Scope, ref string) (string, error) {
		resolved++
		if scope.Kind() != credentialref.ScopeConnection || scope.Identifier() != value.ID().String() || ref != "env:SYNTHETIC" {
			t.Error("wrong credential owner")
		}
		if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 15*time.Second {
			t.Error("unbounded check")
		}
		return "synthetic-token", nil
	}, server.Client())
	if err != nil || check.ErrorCode() != connections.CheckOK || check.Authentication() != connections.AuthenticationAccepted || check.Reachability() != connections.Reachable || len(check.Capabilities()) != 1 || check.Capabilities()[0] != connections.CapabilitySearch || resolved != 1 {
		t.Fatalf("observation=%v err=%v", check, err)
	}
}

func TestConnectionSearchCheckRejectsUnprovenCapabilities(t *testing.T) {
	for _, test := range []struct {
		name, body string
		status     int
		failure    connections.CheckError
		auth       connections.Authentication
	}{
		{"empty-results", `{"results":[]}`, 200, connections.CheckOK, connections.AuthenticationNotRequired},
		{"missing-results", `{}`, 200, connections.CheckInvalidResponse, connections.AuthenticationUnknown},
		{"null-results", `{"results":null}`, 200, connections.CheckInvalidResponse, connections.AuthenticationUnknown},
		{"wrong-results", `{"results":{}}`, 200, connections.CheckInvalidResponse, connections.AuthenticationUnknown},
		{"malformed", `private upstream text`, 200, connections.CheckInvalidResponse, connections.AuthenticationUnknown},
		{"incomplete-result", `{"results":[{"title":"missing-url"}]}`, 200, connections.CheckInvalidResponse, connections.AuthenticationUnknown},
		{"oversized", strings.Repeat("x", int(defaultMaxResponseBytes)+1), 200, connections.CheckInvalidResponse, connections.AuthenticationUnknown},
		{"unauthorized", `private auth body`, 401, connections.CheckAuthenticationFailed, connections.AuthenticationRejected},
		{"forbidden", `private auth body`, 403, connections.CheckAuthenticationFailed, connections.AuthenticationRejected},
		{"unavailable", `private failure body`, 503, connections.CheckUnavailable, connections.AuthenticationUnknown},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(test.status); w.Write([]byte(test.body)) }))
			defer server.Close()
			check, err := checkConnection(t.Context(), searchCheckConnection(t, server.URL, ""), nil, server.Client())
			if err != nil || check.ErrorCode() != test.failure || check.Authentication() != test.auth || (len(check.Capabilities()) != 0) != (test.failure == connections.CheckOK) {
				t.Fatalf("observation=%v err=%v", check, err)
			}
		})
	}
}

func TestConnectionSearchCheckMissingCredentialNeverQueries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("queried without credential") }))
	defer server.Close()
	value := searchCheckConnection(t, server.URL, "env:SYNTHETIC")
	for _, resolve := range []ConnectionCredentialResolver{nil, func(context.Context, credentialref.Scope, string) (string, error) {
		return "", errors.New("private-secret-error")
	}} {
		check, err := checkConnection(t.Context(), value, resolve, server.Client())
		if err != nil || check.ErrorCode() != connections.CheckCredentialUnavailable {
			t.Fatal(check, err)
		}
	}
}

func TestConnectionSearchCheckGuardsPrivateDestinationsAndMixedDNS(t *testing.T) {
	t.Setenv("NO_PROXY", "*")
	check, err := CheckConnection(t.Context(), searchCheckConnection(t, "http://127.0.0.1:1/search", ""), nil)
	if err != nil || check.ErrorCode() != connections.CheckNetworkDenied {
		t.Fatal(check, err)
	}
	for _, addresses := range [][]netip.Addr{
		nil,
		{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("::ffff:127.0.0.1")},
		{netip.MustParseAddr("100.64.0.1")},
		{netip.MustParseAddr("169.254.169.254")},
		{netip.MustParseAddr("::1")},
	} {
		if publicSearchCheckResolution(addresses) {
			t.Fatal("unsafe DNS resolution admitted")
		}
	}
	if !publicSearchCheckResolution([]netip.Addr{netip.MustParseAddr("8.8.8.8")}) {
		t.Fatal("public address denied")
	}
}

func TestConnectionSearchCheckCancellationAndRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			w.Header().Set("Location", "/private")
			w.WriteHeader(302)
			return
		}
		if r.URL.Path == "/private" {
			t.Error("followed redirect")
			return
		}
		<-r.Context().Done()
	}))
	defer server.Close()
	client := searchCheckClient(server.Client().Transport)
	check, err := checkConnection(t.Context(), searchCheckConnection(t, server.URL+"/redirect", ""), nil, client)
	if err != nil || check.ErrorCode() != connections.CheckUnavailable || len(check.Capabilities()) != 0 {
		t.Fatal(check, err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	check, err = checkConnection(ctx, searchCheckConnection(t, server.URL+"/stall", ""), nil, client)
	if err != nil || check.ErrorCode() != connections.CheckTimeout {
		t.Fatal(check, err)
	}
}
