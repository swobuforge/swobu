package outboundhttp

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"
)

// DialContext dials a direct destination. It is the signature of the guarded
// dialer a restricted caller (media, remote MCP) supplies so its existing
// destination checks run only on a directly-selected route — never on the
// first-hop address of an already-selected proxy.
type DialContext func(
	context.Context,
	string,
	string,
) (net.Conn, error)

// Config configures a Transport. The zero value is not meaningful in production
// because NewTransport type-asserts http.DefaultTransport; construct a
// Transport only through NewTransport.
type Config struct {
	// DirectDialContext enables restricted-direct mode. When nil, the transport
	// is unrestricted and delegates route selection entirely to the cloned
	// standard transport.
	DirectDialContext DialContext

	// ResponseHeaderTimeout overrides the cloned standard value when non-zero.
	ResponseHeaderTimeout time.Duration

	// TLSClientConfig, when non-nil, is cloned before use. Callers must not be
	// able to mutate live transport configuration after construction.
	TLSClientConfig *tls.Config
}

// proxySelector returns the request-level proxy decision. A nil URL with a nil
// error selects a direct route; a non-nil URL selects that proxy. It mirrors
// http.ProxyFromEnvironment's signature so the production selector delegates to
// the standard library without reimplementation. It is unexported so package
// tests can inject selectors without mutating environment variables, while
// production callers always use NewTransport's http.ProxyFromEnvironment.
type proxySelector func(*http.Request) (*url.URL, error)

// selectedProxyContextKey is the private context key that carries the proxy
// decision an outer RoundTrip already made, so the shared proxied transport's
// Proxy func can retrieve it without re-evaluating the selector.
type selectedProxyContextKey struct{}

// Transport is the sole production net/http network transport owner. A
// Transport is either unrestricted (provider) or restricted (media, remote
// MCP); exactly one of the two layouts is present.
type Transport struct {
	selectProxy proxySelector

	// Exactly one of these layouts exists:
	//   unrestricted: standard != nil, direct == nil, proxied == nil
	//   restricted:   standard == nil, direct != nil, proxied != nil
	standard *http.Transport
	direct   *http.Transport
	proxied  *http.Transport
}

// NewTransport constructs the production Transport. The proxy law is always
// Go's request-level http.ProxyFromEnvironment; production callers may not
// select a different proxy law.
func NewTransport(cfg Config) *Transport {
	return newTransport(cfg, http.ProxyFromEnvironment)
}

// newTransport constructs a Transport with an explicit selector. It exists
// solely so package tests can exercise route decisions without mutating
// environment variables. Production callers use NewTransport.
func newTransport(cfg Config, selectProxy proxySelector) *Transport {
	if selectProxy == nil {
		panic("outboundhttp: nil proxy selector")
	}

	base := http.DefaultTransport.(*http.Transport).Clone()
	if cfg.ResponseHeaderTimeout != 0 {
		base.ResponseHeaderTimeout = cfg.ResponseHeaderTimeout
	}
	if cfg.TLSClientConfig != nil {
		base.TLSClientConfig = cfg.TLSClientConfig.Clone()
	}

	if cfg.DirectDialContext == nil {
		base.Proxy = selectProxy
		return &Transport{standard: base}
	}

	direct := base.Clone()
	direct.Proxy = nil
	direct.DialContext = cfg.DirectDialContext

	proxied := base.Clone()
	proxied.Proxy = selectedProxyFromRequestContext

	return &Transport{
		selectProxy: selectProxy,
		direct:      direct,
		proxied:     proxied,
	}
}

// RoundTrip executes a request using the single request-level proxy decision.
// In unrestricted mode the cloned standard transport owns its ordinary
// direct/proxy behavior. In restricted mode it evaluates the selector exactly
// once: a nil result routes to the caller's guarded direct dialer; a non-nil
// result routes to the shared proxied transport, carrying the decision through
// a private context value so the selector is not evaluated again.
func (t *Transport) RoundTrip(
	req *http.Request,
) (*http.Response, error) {
	if t.standard != nil {
		return t.standard.RoundTrip(req)
	}

	proxyURL, err := t.selectProxy(req)
	if err != nil {
		return nil, fmt.Errorf("select outbound proxy: %w", err)
	}
	if proxyURL == nil {
		return t.direct.RoundTrip(req)
	}

	selected := *proxyURL
	ctx := context.WithValue(
		req.Context(),
		selectedProxyContextKey{},
		&selected,
	)
	return t.proxied.RoundTrip(req.WithContext(ctx))
}

// selectedProxyFromRequestContext is the proxied transport's Proxy func. It
// retrieves the prior decision from the request context rather than
// re-evaluating the selector, keeping the route decision atomic.
func selectedProxyFromRequestContext(
	req *http.Request,
) (*url.URL, error) {
	proxyURL, ok := req.Context().Value(
		selectedProxyContextKey{},
	).(*url.URL)
	if !ok || proxyURL == nil {
		return nil, errors.New(
			"outboundhttp: proxied transport called without selected proxy",
		)
	}
	return proxyURL, nil
}

// CloseIdleConnections closes idle connections on every internal transport. It
// is safe before any request and safe to call more than once.
func (t *Transport) CloseIdleConnections() {
	if t == nil {
		return
	}
	if t.standard != nil {
		t.standard.CloseIdleConnections()
	}
	if t.direct != nil {
		t.direct.CloseIdleConnections()
	}
	if t.proxied != nil {
		t.proxied.CloseIdleConnections()
	}
}
