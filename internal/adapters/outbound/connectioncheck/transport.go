package connectioncheck

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"time"

	"github.com/swobuforge/swobu/internal/adapters/outbound/httpedge"
	"github.com/swobuforge/swobu/internal/platform/outboundhttp"
)

const modelCheckBodyLimit = 4 << 20
const modelCheckRequestLimit = 16

var errModelNetworkDenied = errors.New("Models check destination denied")

func newModelTransport() *outboundhttp.Transport {
	return outboundhttp.NewTransport(outboundhttp.Config{
		DirectDialContext:     modelCheckDial,
		ResponseHeaderTimeout: 5 * time.Second,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
	})
}

func modelCheckClient(transport http.RoundTripper) *http.Client {
	return &http.Client{Transport: transport, Timeout: 15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// modelResponseTransport bounds both encoded and decoded bodies before provider
// decoding, and counts pages across the entire check. Evidence contains no URL,
// headers, credential, body or provider error text. It is used serially by one
// discovery; no shared state survives the check.
type modelResponseTransport struct {
	pinOrigin                                        bool
	origin                                           string
	next                                             http.RoundTripper
	requests, received, successful                   int
	authRejected, denied, failed, invalid, badStatus bool
}

func (t *modelResponseTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.requests++
	if t.requests > modelCheckRequestLimit {
		t.failed = true
		return nil, errors.New("Models check request limit exceeded")
	}
	if req.URL == nil || (req.URL.Scheme != "https" && req.URL.Scheme != "http") || req.URL.User != nil || req.URL.Fragment != "" || req.URL.Hostname() == "" {
		t.denied = true
		return nil, errModelNetworkDenied
	}
	if t.pinOrigin {
		origin := req.URL.Scheme + "://" + req.URL.Host
		if t.origin == "" {
			t.origin = origin
		}
		if origin != t.origin {
			t.denied = true
			return nil, errModelNetworkDenied
		}
	}
	response, err := t.next.RoundTrip(req)
	if err != nil {
		t.failed = true
		t.denied = t.denied || errors.Is(err, errModelNetworkDenied)
		return nil, err
	}
	t.received++
	if response.StatusCode == 401 || response.StatusCode == 403 {
		t.authRejected = true
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		t.badStatus, t.failed = true, true
		// Provider error helpers may log backend prose. A check needs only
		// status evidence, so no upstream error body reaches those helpers.
		response.Body.Close()
		response.Body = io.NopCloser(bytes.NewReader(nil))
		response.Header.Del("Content-Encoding")
		response.Header.Del("Content-Length")
		response.ContentLength = 0
		return response, nil
	}
	// Request-owned queries (catalog pagination/API versions) are allowed;
	// redirects and their Location authority are never followed.
	raw, err := io.ReadAll(io.LimitReader(response.Body, modelCheckBodyLimit+1))
	response.Body.Close()
	if err != nil || len(raw) > modelCheckBodyLimit {
		t.invalid, t.failed = true, true
		return nil, errors.New("Models check body unavailable or oversized")
	}
	response.Body = io.NopCloser(bytes.NewReader(raw))
	decoded, err := httpedge.DecodeHTTPResponseContentEncoding(response)
	if err != nil {
		response.Body.Close()
		t.invalid, t.failed = true, true
		return nil, errors.New("Models check encoding invalid")
	}
	body, err := io.ReadAll(io.LimitReader(decoded.Body, modelCheckBodyLimit+1))
	decoded.Body.Close()
	if err != nil || len(body) > modelCheckBodyLimit {
		t.invalid, t.failed = true, true
		return nil, errors.New("Models check decoded body unavailable or oversized")
	}
	decoded.Body = io.NopCloser(bytes.NewReader(body))
	decoded.ContentLength = int64(len(body))
	decoded.Header.Del("Content-Length")
	if decoded.StatusCode >= 200 && decoded.StatusCode < 300 {
		t.successful++
	}
	return decoded, nil
}

func modelCheckDial(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, errModelNetworkDenied
	}
	addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	if !publicModelResolution(addresses) {
		return nil, errModelNetworkDenied
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

func publicModelResolution(addresses []netip.Addr) bool {
	if len(addresses) == 0 {
		return false
	}
	for _, ip := range addresses {
		ip = ip.Unmap()
		if !ip.IsValid() || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsMulticast() || ip.IsUnspecified() {
			return false
		}
		for _, prefix := range prohibitedModelPrefixes {
			if prefix.Contains(ip) {
				return false
			}
		}
	}
	return true
}

var prohibitedModelPrefixes = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("2001:db8::/32"),
}
