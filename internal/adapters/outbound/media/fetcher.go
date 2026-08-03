// Package media owns credential-free, policy-bounded image materialization for
// provider-attempt preparation. It performs no provider request encoding.
package media

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/platform/outboundhttp"
	"github.com/swobuforge/swobu/internal/provider"
)

type PublicImageFetcher struct{ Resolver *net.Resolver }

func NewPublicImageFetcher() PublicImageFetcher {
	return PublicImageFetcher{Resolver: net.DefaultResolver}
}

func (f PublicImageFetcher) FetchImage(ctx context.Context, source canonical.URLImage, policy provider.NetworkPolicy, maxBytes int64) (provider.FetchedImageResult, error) {
	rawURL := source.String()
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return provider.FetchedImageResult{}, fmt.Errorf("media URL is not an unauthenticated HTTP(S) URL")
	}
	if err := authorizeHostname(parsed.Hostname(), policy); err != nil {
		return provider.FetchedImageResult{}, err
	}
	timeout := policy.PerImageTimeout
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	resolver := f.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	// outboundhttp owns proxy route selection via Go's request-level
	// ProxyFromEnvironment. The restricted-direct guarded dialer runs only when
	// the per-request decision is "direct" — never on the first-hop address of an
	// already-selected proxy. URL/scheme/redirect authority still runs on every
	// request and redirect, independent of dialing.
	transport := outboundhttp.NewTransport(outboundhttp.Config{DirectDialContext: authorizedDialer(resolver, policy)})
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > effectiveRedirectLimit(policy) {
			return fmt.Errorf("media URL exceeds redirect limit")
		}
		if req.URL.User != nil || (req.URL.Scheme != "http" && req.URL.Scheme != "https") {
			return fmt.Errorf("media redirect is not an unauthenticated HTTP(S) URL")
		}
		if via[len(via)-1].URL.Scheme == "https" && req.URL.Scheme == "http" && !policy.AllowHTTPSDowngrade {
			return fmt.Errorf("media redirect downgrades HTTPS")
		}
		return authorizeHostname(req.URL.Hostname(), policy)
	}
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return provider.FetchedImageResult{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return provider.FetchedImageResult{}, fmt.Errorf("media fetch failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return provider.FetchedImageResult{}, fmt.Errorf("media fetch returned HTTP %d", resp.StatusCode)
	}
	declared := strings.ToLower(strings.TrimSpace(strings.Split(resp.Header.Get("Content-Type"), ";")[0])) // swobu:io-string source=provider-wire
	if declared != "" && declared != "application/octet-stream" && !strings.HasPrefix(declared, "image/") {
		return provider.FetchedImageResult{}, fmt.Errorf("media fetch returned explicit non-image content type %q", declared)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return provider.FetchedImageResult{}, err
	}
	if int64(len(data)) > maxBytes {
		return provider.FetchedImageResult{}, fmt.Errorf("media image exceeds byte limit")
	}
	var declaredMediaType canonical.ImageMediaType
	if declared != "" && declared != "application/octet-stream" {
		if declared == "image/jpg" {
			declared = string(canonical.ImageMediaJPEG)
		}
		declaredMediaType = canonical.ImageMediaType(declared)
	}
	return provider.FetchedImageResult{Bytes: data, DeclaredMediaType: declaredMediaType}, nil
}

func authorizedDialer(resolver *net.Resolver, policy provider.NetworkPolicy) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		if err := authorizeHostname(host, policy); err != nil {
			return nil, err
		}
		addresses, err := resolver.LookupIPAddr(ctx, host)
		if err != nil || len(addresses) == 0 {
			return nil, fmt.Errorf("media hostname resolution failed")
		}
		for _, resolved := range addresses {
			if !authorizedIP(resolved.IP, host, policy) {
				continue
			}
			return (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort(resolved.IP.String(), port))
		}
		return nil, fmt.Errorf("media hostname resolved only to denied addresses")
	}
}

func authorizeHostname(host string, policy provider.NetworkPolicy) error {
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), ".")) // swobu:io-string source=boundary
	if host == "" {
		return fmt.Errorf("media hostname is denied")
	}
	if matchesHost(host, policy.DeniedHosts) {
		return fmt.Errorf("media hostname is denied")
	}
	if policy.Access == provider.NetworkAllowlisted {
		if !matchesHost(host, policy.AllowedHosts) {
			return fmt.Errorf("media hostname is not allowlisted")
		}
		return nil
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") {
		return fmt.Errorf("media hostname is denied")
	}
	return nil
}

func authorizedIP(ip net.IP, host string, policy provider.NetworkPolicy) bool {
	if policy.Access == provider.NetworkAllowlisted && matchesHost(host, policy.AllowedHosts) {
		return !ip.IsUnspecified() && !ip.IsMulticast()
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return false
	}
	_, cgnat, _ := net.ParseCIDR("100.64.0.0/10")
	return !cgnat.Contains(ip)
}

func matchesHost(host string, patterns []provider.HostPattern) bool {
	for _, pattern := range patterns {
		if pattern.Matches(host) {
			return true
		}
	}
	return false
}

func effectiveRedirectLimit(policy provider.NetworkPolicy) int {
	return policy.MaxRedirects
}
