package search

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/swobuforge/swobu/internal/connections"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
	"github.com/swobuforge/swobu/internal/platform/outboundhttp"
)

// ConnectionCredentialResolver resolves only the inspected Connection's binding.
// Search provider labels never select credential ownership.
type ConnectionCredentialResolver func(context.Context, credentialref.Scope, string) (string, error)

// CheckConnection performs one synthetic qualified-JSON query. It retains no
// results, creates no Workspace operation and reports only revision-bound facts.
// Direct dials reject any prohibited DNS answer and dial the admitted IP itself;
// configured proxies retain outboundhttp's request-level environment policy.
func CheckConnection(ctx context.Context, value connections.Connection, resolve ConnectionCredentialResolver) (connections.Check, error) {
	transport := outboundhttp.NewTransport(outboundhttp.Config{
		DirectDialContext:     searchCheckDial,
		ResponseHeaderTimeout: 5 * time.Second,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
	})
	defer transport.CloseIdleConnections()
	client := searchCheckClient(transport)
	return checkConnection(ctx, value, resolve, client)
}

func searchCheckClient(transport http.RoundTripper) *http.Client {
	return &http.Client{Transport: transport, Timeout: 15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func checkConnection(ctx context.Context, value connections.Connection, resolve ConnectionCredentialResolver, client *http.Client) (connections.Check, error) {
	spec, ok := value.Spec().(connections.SearchSpec)
	if !ok {
		return connections.Check{}, errors.New("Search Connection required")
	}
	observe := func(reach connections.Reachability, auth connections.Authentication, capabilities []connections.Capability, failure connections.CheckError) (connections.Check, error) {
		return connections.NewCheck(value, time.Now().UTC(), reach, auth, capabilities, failure)
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	source, err := NewJSONSource(spec.Endpoint().String(), client)
	if err != nil {
		return observe(connections.ReachabilityUnknown, connections.AuthenticationUnknown, nil, connections.CheckNetworkDenied)
	}
	authenticated := spec.Credential().Kind() != credentialref.KindEmpty
	if authenticated {
		scope, err := credentialref.NewConnectionScope(value.ID().String())
		if err != nil {
			return connections.Check{}, err
		}
		if resolve == nil {
			return observe(connections.ReachabilityUnknown, connections.AuthenticationUnknown, nil, connections.CheckCredentialUnavailable)
		}
		token, err := resolve(ctx, scope, spec.Credential().String())
		if err != nil || strings.TrimSpace(token) == "" {
			return observe(connections.ReachabilityUnknown, connections.AuthenticationUnknown, nil, connections.CheckCredentialUnavailable)
		}
		source = source.WithBearer(token)
	}
	// Use the runtime adapter's exact fixed-endpoint query construction. Capture
	// response status without retaining upstream prose or duplicating auth policy.
	status := 0
	boundedClient := *client
	boundedClient.Transport = searchCheckResponseTransport{next: client.Transport, status: &status}
	source.Client = &boundedClient
	_, err = source.Search(ctx, Query{Text: "swobu connection check", Limit: 1})
	if err != nil {
		switch {
		case status == http.StatusUnauthorized || status == http.StatusForbidden:
			return observe(connections.Reachable, connections.AuthenticationRejected, nil, connections.CheckAuthenticationFailed)
		case errors.Is(err, errSearchCheckNetworkDenied):
			return observe(connections.ReachabilityUnknown, connections.AuthenticationUnknown, nil, connections.CheckNetworkDenied)
		case errors.Is(ctx.Err(), context.DeadlineExceeded), errors.Is(err, context.DeadlineExceeded):
			return observe(connections.ReachabilityUnknown, connections.AuthenticationUnknown, nil, connections.CheckTimeout)
		case status >= 200 && status < 300:
			return observe(connections.Reachable, connections.AuthenticationUnknown, nil, connections.CheckInvalidResponse)
		case status != 0:
			return observe(connections.Reachable, connections.AuthenticationUnknown, nil, connections.CheckUnavailable)
		default:
			return observe(connections.ReachabilityUnknown, connections.AuthenticationUnknown, nil, connections.CheckUnavailable)
		}
	}
	auth := connections.AuthenticationNotRequired
	if authenticated {
		auth = connections.AuthenticationAccepted
	}
	return observe(connections.Reachable, auth, []connections.Capability{connections.CapabilitySearch}, connections.CheckOK)
}

// The runtime decoder accepts absent results as empty. Discovery requires an
// actual results array, so arbitrary JSON success cannot establish capability.
type searchCheckResponseTransport struct {
	next   http.RoundTripper
	status *int
}

func (t searchCheckResponseTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	next := t.next
	if next == nil {
		next = http.DefaultTransport
	}
	response, err := next.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	*t.status = response.StatusCode
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return response, nil
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, defaultMaxResponseBytes+1))
	response.Body.Close()
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > defaultMaxResponseBytes {
		return nil, errors.New("Search check response limit exceeded")
	}
	var envelope struct {
		Results json.RawMessage `json:"results"`
	}
	if json.Unmarshal(body, &envelope) != nil || len(envelope.Results) == 0 || envelope.Results[0] != '[' {
		return nil, errors.New("Search check requires a results array")
	}
	response.Body = io.NopCloser(strings.NewReader(string(body)))
	return response, nil
}

var errSearchCheckNetworkDenied = errors.New("Search check destination denied")

func searchCheckDial(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, errSearchCheckNetworkDenied
	}
	addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	if !publicSearchCheckResolution(addresses) {
		return nil, errSearchCheckNetworkDenied
	}
	var last error
	for _, ip := range addresses {
		conn, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.Unmap().String(), port))
		if err == nil {
			return conn, nil
		}
		last = err
	}
	return nil, last
}

func publicSearchCheckResolution(addresses []netip.Addr) bool {
	if len(addresses) == 0 {
		return false
	}
	for _, ip := range addresses {
		ip = ip.Unmap()
		if !ip.IsValid() || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsMulticast() || ip.IsUnspecified() {
			return false
		}
		for _, prefix := range prohibitedSearchCheckPrefixes {
			if prefix.Contains(ip) {
				return false
			}
		}
	}
	return true
}

var prohibitedSearchCheckPrefixes = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("2001:db8::/32"),
}
