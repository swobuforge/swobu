package bedrock

import (
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// validateBedrockMantleEndpoint is the endpoint host gate. Three host classes
// are all allowed: a canonical AWS Mantle host, a loopback/test host, and any
// other (custom or PrivateLink) host. Swobu never fails-closed merely because a
// host is unknown — only canonical Mantle has a verified SigV4 contract, so an
// unknown host is allowed with that contract unverified. For a known Mantle
// host, a region that contradicts the host's implied region is an actionable
// error; an absent region is not (the signing region is a separate fact).
func validateBedrockMantleEndpoint(baseURL, region string) error {
	host := trimBedrockInput(mustParseURL(baseURL).Hostname())
	if host == "" {
		return canonical.BadEndpoint("bedrock provider requires an endpoint host")
	}
	if isLoopbackOrTestHost(host) {
		return nil
	}
	hostRegion := bedrockHostRegion(baseURL)
	if hostRegion == "" {
		return nil
	}
	region = strings.TrimSpace(region)
	if region != "" && !strings.EqualFold(hostRegion, region) {
		return canonical.BadEndpoint("bedrock endpoint region " + hostRegion + " does not match signing region " + region)
	}
	return nil
}

// isLoopbackOrTestHost reports whether a host is a local test address. These
// hosts bypass region↔host consistency since they carry no AWS region.
func isLoopbackOrTestHost(host string) bool {
	switch host {
	case "localhost", "127.0.0.1", "::1", "example.test":
		return true
	}
	return false
}
