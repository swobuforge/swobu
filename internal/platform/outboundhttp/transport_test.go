package outboundhttp

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

// fakeProxy is a minimal forward HTTP proxy: it serves a canned response for
// every proxied request, recording that it was used. It listens on loopback so
// a request to an unresolvable destination that is routed through it still
// succeeds.
type fakeProxy struct {
	server   *httptest.Server
	used     atomic.Bool
	respond  func(w http.ResponseWriter)
	dialed   atomic.Bool
	proxyURL *url.URL
}

func newFakeProxy(t *testing.T, respond func(w http.ResponseWriter)) *fakeProxy {
	t.Helper()
	fp := &fakeProxy{respond: respond}
	fp.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fp.used.Store(true)
		if fp.respond != nil {
			fp.respond(w)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(fp.server.Close)
	u, err := url.Parse(fp.server.URL)
	if err != nil {
		t.Fatalf("parse fake proxy url: %v", err)
	}
	fp.proxyURL = u
	return fp
}

// recordedDialer is a guarded DirectDialContext that records its invocation and
// optionally redirects the dial to a local server's address, so a direct route
// can be observed without depending on real DNS.
type recordedDialer struct {
	called   atomic.Bool
	redirect string // when non-empty, dial this address instead of the requested one
}

func (d *recordedDialer) dial(ctx context.Context, network, address string) (net.Conn, error) {
	d.called.Store(true)
	if d.redirect != "" {
		address = d.redirect
	}
	var nd net.Dialer
	return nd.DialContext(ctx, network, address)
}

func (d *recordedDialer) failIfCalled(ctx context.Context, network, address string) (net.Conn, error) {
	d.called.Store(true)
	return nil, errors.New("guarded dialer must not be called on a proxied route")
}

func readBody(t *testing.T, r *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(r.Body)
	_ = r.Body.Close()
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}

// A. TestTransport_UnrestrictedUsesSelectedProxy
func TestTransport_UnrestrictedUsesSelectedProxy(t *testing.T) {
	fp := newFakeProxy(t, func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "from-proxy")
	})
	tr := newTransport(Config{}, func(*http.Request) (*url.URL, error) {
		return fp.proxyURL, nil
	})
	t.Cleanup(tr.CloseIdleConnections)

	client := &http.Client{Transport: tr}
	resp, err := client.Get("http://destination.invalid/path")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if body := readBody(t, resp); body != "from-proxy" {
		t.Fatalf("body = %q, want %q", body, "from-proxy")
	}
	if !fp.used.Load() {
		t.Fatal("unrestricted transport did not route through the selected proxy")
	}
}

// B. TestTransport_RestrictedDirectUsesGuardedDialer
func TestTransport_RestrictedDirectUsesGuardedDialer(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "from-direct")
	}))
	t.Cleanup(upstream.Close)

	d := &recordedDialer{redirect: upstream.Listener.Addr().String()}
	tr := newTransport(Config{DirectDialContext: d.dial}, func(*http.Request) (*url.URL, error) {
		return nil, nil
	})
	t.Cleanup(tr.CloseIdleConnections)

	client := &http.Client{Transport: tr}
	resp, err := client.Get("http://restricted.invalid/path")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if body := readBody(t, resp); body != "from-direct" {
		t.Fatalf("body = %q, want %q", body, "from-direct")
	}
	if !d.called.Load() {
		t.Fatal("restricted transport did not invoke the guarded direct dialer on a nil selector")
	}
}

// C. TestTransport_RestrictedProxySkipsGuardedDialer
func TestTransport_RestrictedProxySkipsGuardedDialer(t *testing.T) {
	fp := newFakeProxy(t, func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "from-proxy")
	})
	d := &recordedDialer{}
	tr := newTransport(Config{DirectDialContext: d.failIfCalled}, func(*http.Request) (*url.URL, error) {
		return fp.proxyURL, nil
	})
	t.Cleanup(tr.CloseIdleConnections)

	client := &http.Client{Transport: tr}
	resp, err := client.Get("http://restricted.invalid/path")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if d.called.Load() {
		t.Fatal("guarded dialer must not be called on a proxied route")
	}
	if !fp.used.Load() {
		t.Fatal("restricted proxied transport did not route through the selected proxy")
	}
}

// D. TestTransport_SelectsProxyExactlyOnce
func TestTransport_SelectsProxyExactlyOnce(t *testing.T) {
	fp := newFakeProxy(t, nil)
	var calls atomic.Int64
	tr := newTransport(Config{DirectDialContext: (&recordedDialer{}).dial}, func(*http.Request) (*url.URL, error) {
		calls.Add(1)
		return fp.proxyURL, nil
	})
	t.Cleanup(tr.CloseIdleConnections)

	client := &http.Client{Transport: tr}
	resp, err := client.Get("http://restricted.invalid/path")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = resp.Body.Close()
	if got := calls.Load(); got != 1 {
		t.Fatalf("selector called %d times, want exactly 1", got)
	}
}

// E. TestTransport_SelectorErrorStopsBeforeDial
func TestTransport_SelectorErrorStopsBeforeDial(t *testing.T) {
	sentinel := errors.New("selector failure")
	d := &recordedDialer{}
	fp := newFakeProxy(t, nil)
	tr := newTransport(Config{DirectDialContext: d.failIfCalled}, func(*http.Request) (*url.URL, error) {
		return nil, sentinel
	})
	t.Cleanup(tr.CloseIdleConnections)
	t.Cleanup(fp.server.Close)

	client := &http.Client{Transport: tr}
	_, err := client.Get("http://restricted.invalid/path")
	if err == nil {
		t.Fatal("expected an error from a failed selector, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("error does not wrap the selector sentinel: %v", err)
	}
	if d.called.Load() {
		t.Fatal("direct dialer was dialed despite a selector error")
	}
	if fp.used.Load() {
		t.Fatal("proxy was contacted despite a selector error")
	}
}

// F. TestTransport_DirectDestinationMayEqualProxyAddress — the P0 regression.
// The request URL targets an address that is also conceptually a proxy
// endpoint; the selector returns nil and the guarded direct dialer must still
// run and succeed directly, never classifying the route by address equality.
func TestTransport_DirectDestinationMayEqualProxyAddress(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "from-direct")
	}))
	t.Cleanup(upstream.Close)
	upstreamAddr := upstream.Listener.Addr().String()

	d := &recordedDialer{redirect: upstreamAddr}
	tr := newTransport(Config{DirectDialContext: d.dial}, func(*http.Request) (*url.URL, error) {
		// Even though the destination address equals a configured proxy
		// endpoint, the selector selects direct access.
		return nil, nil
	})
	t.Cleanup(tr.CloseIdleConnections)

	client := &http.Client{Transport: tr}
	// The request URL carries the "proxy endpoint" address as its destination.
	resp, err := client.Get("http://" + upstreamAddr + "/admin")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if body := readBody(t, resp); body != "from-direct" {
		t.Fatalf("body = %q, want %q", body, "from-direct")
	}
	if !d.called.Load() {
		t.Fatal("guarded dialer did not run for a direct destination equal to the proxy address")
	}
}

// G. TestTransport_ConfigurationAppliedToEveryBranch
func TestTransport_ConfigurationAppliedToEveryBranch(t *testing.T) {
	const timeout = 250 * time.Millisecond
	cfgTLS := &tls.Config{MinVersion: tls.VersionTLS12}

	// Unrestricted: the single standard branch receives configuration.
	unrestricted := newTransport(Config{ResponseHeaderTimeout: timeout, TLSClientConfig: cfgTLS}, func(*http.Request) (*url.URL, error) {
		return nil, nil
	})
	if unrestricted.standard == nil || unrestricted.direct != nil || unrestricted.proxied != nil {
		t.Fatal("unrestricted transport must have only the standard branch")
	}
	if unrestricted.standard.ResponseHeaderTimeout != timeout {
		t.Fatalf("unrestricted ResponseHeaderTimeout = %v, want %v", unrestricted.standard.ResponseHeaderTimeout, timeout)
	}
	if unrestricted.standard.TLSClientConfig == nil || unrestricted.standard.TLSClientConfig.MinVersion != tls.VersionTLS12 {
		t.Fatal("unrestricted standard branch did not receive a cloned TLS config")
	}

	// Restricted: direct and proxied branches both receive configuration.
	restricted := newTransport(Config{
		ResponseHeaderTimeout: timeout,
		TLSClientConfig:       cfgTLS,
		DirectDialContext:     (&recordedDialer{}).dial,
	}, func(*http.Request) (*url.URL, error) {
		return nil, nil
	})
	if restricted.standard != nil || restricted.direct == nil || restricted.proxied == nil {
		t.Fatal("restricted transport must have direct and proxied branches, no standard")
	}
	for name, branch := range map[string]*http.Transport{"direct": restricted.direct, "proxied": restricted.proxied} {
		if branch.ResponseHeaderTimeout != timeout {
			t.Fatalf("%s ResponseHeaderTimeout = %v, want %v", name, branch.ResponseHeaderTimeout, timeout)
		}
		if branch.TLSClientConfig == nil || branch.TLSClientConfig.MinVersion != tls.VersionTLS12 {
			t.Fatalf("%s branch did not receive a cloned TLS config", name)
		}
	}
	if restricted.direct.TLSClientConfig == restricted.proxied.TLSClientConfig {
		t.Fatal("direct and proxied branches share a TLS config pointer; each must own an independent clone")
	}

	// Mutating the original caller config after construction must not change
	// live branches (callers cannot mutate live transport configuration).
	cfgTLS.MinVersion = tls.VersionTLS13
	if restricted.direct.TLSClientConfig.MinVersion != tls.VersionTLS12 {
		t.Fatal("mutating the caller TLS config after construction changed a live branch")
	}
}

// H. TestTransport_CloseIdleConnectionsIsSafe
func TestTransport_CloseIdleConnectionsIsSafe(t *testing.T) {
	fp := newFakeProxy(t, nil)
	tr := newTransport(Config{DirectDialContext: (&recordedDialer{}).dial}, func(*http.Request) (*url.URL, error) {
		return fp.proxyURL, nil
	})

	// Before any request, twice.
	tr.CloseIdleConnections()
	tr.CloseIdleConnections()

	client := &http.Client{Transport: tr}
	if resp, err := client.Get("http://restricted.invalid/path"); err != nil {
		t.Fatalf("proxied request: unexpected error: %v", err)
	} else {
		_ = resp.Body.Close()
	}

	// After requests, twice.
	tr.CloseIdleConnections()
	tr.CloseIdleConnections()
}
