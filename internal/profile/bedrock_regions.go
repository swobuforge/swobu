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
	{ID: "ap-northeast-1", Label: "ap-northeast-1", Keywords: []string{"Tokyo"}},
	{ID: "ap-south-1", Label: "ap-south-1", Keywords: []string{"Mumbai"}},
	{ID: "ap-southeast-2", Label: "ap-southeast-2", Keywords: []string{"Sydney"}},
	{ID: "ap-southeast-3", Label: "ap-southeast-3", Keywords: []string{"Jakarta"}},
	{ID: "eu-central-1", Label: "eu-central-1", Keywords: []string{"Frankfurt"}},
	{ID: "eu-north-1", Label: "eu-north-1", Keywords: []string{"Stockholm"}},
	{ID: "eu-south-1", Label: "eu-south-1", Keywords: []string{"Milan"}},
	{ID: "eu-west-1", Label: "eu-west-1", Keywords: []string{"Ireland"}},
	{ID: "eu-west-2", Label: "eu-west-2", Keywords: []string{"London"}},
	{ID: "sa-east-1", Label: "sa-east-1", Keywords: []string{"Sao Paulo"}},
	{ID: "us-east-1", Label: "us-east-1", Keywords: []string{"N. Virginia"}},
	{ID: "us-east-2", Label: "us-east-2", Keywords: []string{"Ohio"}},
	{ID: "us-west-2", Label: "us-west-2", Keywords: []string{"Oregon"}},
}

// BedrockMantleRegions returns the canonical region catalog in stable order.
func BedrockMantleRegions() []BedrockMantleRegionSpec {
	return slices.Clone(bedrockMantleRegionCatalog)
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
