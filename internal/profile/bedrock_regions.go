package profile

import (
	"net/url"
	"strings"

	"slices"
)

// BedrockMantleRegionSpec is one canonical Mantle region entry.
//
// Label stays short for picker rows; Keywords carry city aliases and other
// search hints so the picker stays searchable without inventing a second row
// column.
type BedrockMantleRegionSpec struct {
	ID       string
	Label    string
	Keywords []string
}

var bedrockMantleRegionCatalog = []BedrockMantleRegionSpec{
	{ID: "ap-northeast-1", Label: "Asia Pacific (Tokyo) · ap-northeast-1", Keywords: []string{"Tokyo"}},
	{ID: "ap-south-1", Label: "Asia Pacific (Mumbai) · ap-south-1", Keywords: []string{"Mumbai"}},
	{ID: "ap-southeast-2", Label: "Asia Pacific (Sydney) · ap-southeast-2", Keywords: []string{"Sydney"}},
	{ID: "ap-southeast-3", Label: "Asia Pacific (Jakarta) · ap-southeast-3", Keywords: []string{"Jakarta"}},
	{ID: "eu-central-1", Label: "Europe (Frankfurt) · eu-central-1", Keywords: []string{"Frankfurt"}},
	{ID: "eu-north-1", Label: "Europe (Stockholm) · eu-north-1", Keywords: []string{"Stockholm"}},
	{ID: "eu-south-1", Label: "Europe (Milan) · eu-south-1", Keywords: []string{"Milan"}},
	{ID: "eu-west-1", Label: "Europe (Ireland) · eu-west-1", Keywords: []string{"Ireland"}},
	{ID: "eu-west-2", Label: "Europe (London) · eu-west-2", Keywords: []string{"London"}},
	{ID: "sa-east-1", Label: "South America (São Paulo) · sa-east-1", Keywords: []string{"Sao Paulo", "São Paulo"}},
	{ID: "us-east-1", Label: "US East (N. Virginia) · us-east-1", Keywords: []string{"N. Virginia"}},
	{ID: "us-east-2", Label: "US East (Ohio) · us-east-2", Keywords: []string{"Ohio"}},
	{ID: "us-west-2", Label: "US West (Oregon) · us-west-2", Keywords: []string{"Oregon"}},
}

// BedrockMantleRegions returns the canonical region catalog in stable order.
func BedrockMantleRegions() []BedrockMantleRegionSpec {
	return slices.Clone(bedrockMantleRegionCatalog)
}

// BedrockMantleRegionLabel returns the operator-facing geography plus its
// durable region ID, or the trimmed ID when the catalog does not contain it.
func BedrockMantleRegionLabel(region string) string {
	normalized := strings.TrimSpace(strings.ToLower(region))
	for _, entry := range bedrockMantleRegionCatalog {
		if entry.ID == normalized {
			return entry.Label
		}
	}
	return strings.TrimSpace(region)
}

// SupportsBedrockMantleRegion reports whether a region is in the canonical
// Mantle list.
func SupportsBedrockMantleRegion(region string) bool {
	normalized := strings.TrimSpace(strings.ToLower(region))
	if normalized == "" {
		return false
	}
	for _, entry := range bedrockMantleRegionCatalog {
		if strings.EqualFold(entry.ID, normalized) {
			return true
		}
	}
	return false
}

// BedrockMantleEndpointForRegion derives the canonical Mantle endpoint URL
// for one supported region.
func BedrockMantleEndpointForRegion(region string) string {
	normalized := strings.TrimSpace(strings.ToLower(region))
	if normalized == "" {
		return ""
	}
	return "https://bedrock-mantle." + normalized + ".api.aws/v1"
}

// BedrockMantleRegionFromEndpoint extracts a Mantle region from a canonical
// endpoint URL, or returns empty when the URL is not a Mantle host.
func BedrockMantleRegionFromEndpoint(baseURL string) string {
	trimmed := strings.TrimSpace(baseURL)
	if trimmed == "" {
		return ""
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return ""
	}
	host := strings.ToLower(strings.TrimSpace(u.Hostname()))
	if host == "" {
		return ""
	}
	parts := strings.Split(host, ".")
	if len(parts) != 4 {
		return ""
	}
	if !strings.HasPrefix(parts[0], "bedrock-mantle") {
		return ""
	}
	if parts[2] != "api" || parts[3] != "aws" {
		return ""
	}
	region := strings.TrimSpace(parts[1])
	if region == "" {
		return ""
	}
	return region
}
