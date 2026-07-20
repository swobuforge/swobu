package provider

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

type NetworkAccess string

const (
	NetworkPublicOnly  NetworkAccess = "public_only"
	NetworkAllowlisted NetworkAccess = "allowlisted"
)

// HostPattern is one validated exact host or leading-wildcard suffix.
type HostPattern struct {
	exact  string
	suffix string
}

func NewHostPattern(raw string) (HostPattern, error) {
	normalized := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(raw), ".")) // swobu:io-string source=boundary
	if normalized == "" || strings.ContainsAny(normalized, "/:@?#") {
		return HostPattern{}, fmt.Errorf("media host pattern %q is invalid", raw)
	}
	if strings.HasPrefix(normalized, "*.") {
		suffix := strings.TrimPrefix(normalized, "*")
		if len(suffix) < 2 || strings.Contains(suffix, "*") {
			return HostPattern{}, fmt.Errorf("media host pattern %q is invalid", raw)
		}
		return HostPattern{suffix: suffix}, nil
	}
	if strings.Contains(normalized, "*") {
		return HostPattern{}, fmt.Errorf("media host pattern %q is invalid", raw)
	}
	return HostPattern{exact: normalized}, nil
}

func (p HostPattern) Matches(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), ".")) // swobu:io-string source=boundary
	return p.exact != "" && host == p.exact || p.suffix != "" && strings.HasSuffix(host, p.suffix) && host != strings.TrimPrefix(p.suffix, ".")
}

func (p HostPattern) valid() bool { return (p.exact == "") != (p.suffix == "") }

type NetworkPolicy struct {
	Access              NetworkAccess
	AllowedHosts        []HostPattern
	DeniedHosts         []HostPattern
	MaxRedirects        int
	PerImageTimeout     time.Duration
	AllowHTTPSDowngrade bool
}

// MediaLimits bounds image inspection and materialization independently of
// ingress transport and replay-retention policy.
type MediaLimits struct {
	MaxImages          int
	MaxImageBytes      int64
	MaxTotalImageBytes int64
	MaxPixelsPerImage  int64
	MaxImageDimension  int
}

type ImageFetchPolicy struct {
	network                 *NetworkPolicy
	totalPreparationTimeout time.Duration
}

func (p ImageFetchPolicy) Clone() ImageFetchPolicy {
	if p.network == nil {
		return ImageFetchPolicy{}
	}
	network := cloneNetworkPolicy(*p.network)
	return ImageFetchPolicy{network: &network, totalPreparationTimeout: p.totalPreparationTimeout}
}

func (p ImageFetchPolicy) Validate() error {
	if p.network == nil {
		if p.totalPreparationTimeout != 0 {
			return fmt.Errorf("disabled image fetch policy cannot contain a preparation timeout")
		}
		return nil
	}
	if p.totalPreparationTimeout <= 0 {
		return fmt.Errorf("image fetch policy requires a positive total preparation timeout")
	}
	return validateNetworkPolicy(*p.network)
}

// NewImageFetchPolicy enables materialization under one validated network policy.
func NewImageFetchPolicy(network NetworkPolicy, totalPreparationTimeout time.Duration) (ImageFetchPolicy, error) {
	if err := validateNetworkPolicy(network); err != nil {
		return ImageFetchPolicy{}, err
	}
	if totalPreparationTimeout <= 0 {
		return ImageFetchPolicy{}, fmt.Errorf("image fetch policy requires a positive total preparation timeout")
	}
	cloned := cloneNetworkPolicy(network)
	return ImageFetchPolicy{network: &cloned, totalPreparationTimeout: totalPreparationTimeout}, nil
}

// DisabledImageFetchPolicy forbids URL materialization and carries no
// unreachable network configuration.
func DisabledImageFetchPolicy() ImageFetchPolicy { return ImageFetchPolicy{} }

func (p ImageFetchPolicy) NetworkPolicy() (NetworkPolicy, bool) {
	if p.network == nil {
		return NetworkPolicy{}, false
	}
	return cloneNetworkPolicy(*p.network), true
}

func (p ImageFetchPolicy) TotalPreparationTimeout() time.Duration {
	return p.totalPreparationTimeout
}

func validateNetworkPolicy(network NetworkPolicy) error {
	if network.Access != NetworkPublicOnly && network.Access != NetworkAllowlisted {
		return fmt.Errorf("image fetch network access %q is invalid", network.Access)
	}
	if network.Access == NetworkPublicOnly && len(network.AllowedHosts) != 0 {
		return fmt.Errorf("public-only image fetch policy cannot contain an allowlist")
	}
	if network.Access == NetworkAllowlisted && len(network.AllowedHosts) == 0 {
		return fmt.Errorf("allowlisted image fetch policy requires hosts")
	}
	if network.MaxRedirects <= 0 || network.PerImageTimeout <= 0 {
		return fmt.Errorf("image fetch policy requires positive redirect and timeout limits")
	}
	for _, patterns := range [][]HostPattern{network.AllowedHosts, network.DeniedHosts} {
		for _, pattern := range patterns {
			if !pattern.valid() {
				return fmt.Errorf("image fetch policy contains an invalid host pattern")
			}
		}
	}
	return nil
}

func cloneNetworkPolicy(network NetworkPolicy) NetworkPolicy {
	network.AllowedHosts = append([]HostPattern(nil), network.AllowedHosts...)
	network.DeniedHosts = append([]HostPattern(nil), network.DeniedHosts...)
	return network
}

func (l MediaLimits) Validate() error {
	if l.MaxImages <= 0 || l.MaxImageBytes <= 0 || l.MaxTotalImageBytes <= 0 || l.MaxPixelsPerImage <= 0 || l.MaxImageDimension <= 0 {
		return fmt.Errorf("media limits must be positive")
	}
	if l.MaxImageBytes > 0 && l.MaxTotalImageBytes > 0 && l.MaxImageBytes > l.MaxTotalImageBytes {
		return fmt.Errorf("per-image byte limit exceeds aggregate image limit")
	}
	return nil
}

func DefaultImageFetchPolicy() ImageFetchPolicy {
	policy, err := NewImageFetchPolicy(NetworkPolicy{Access: NetworkPublicOnly, MaxRedirects: 3, PerImageTimeout: 15 * time.Second}, 60*time.Second)
	if err != nil {
		panic(err)
	}
	return policy
}

func DefaultMediaLimits() MediaLimits {
	return MediaLimits{MaxImages: 32, MaxImageBytes: 8 << 20, MaxTotalImageBytes: 32 << 20, MaxPixelsPerImage: 40_000_000, MaxImageDimension: 16_384}
}

// FetchedImageResult contains bounded response bytes and the origin's explicit media
// declaration when present. Pure inspection determines the actual image type.
type FetchedImageResult struct {
	Bytes             []byte
	DeclaredMediaType canonical.ImageMediaType
}

// ImageFetcher performs only authorized, bounded URL I/O.
type ImageFetcher interface {
	FetchImage(context.Context, canonical.URLImage, NetworkPolicy, int64) (FetchedImageResult, error)
}
