package routing

import (
	_ "embed"
	"encoding/json"
	"slices"
	"strings"
	"sync"
)

//go:embed data/bedrock_regions.json
var bedrockRegionsRaw []byte

type bedrockRegionsSnapshot struct {
	Regions []string `json:"regions"`
}

var (
	loadBedrockRegionsOnce sync.Once
	loadBedrockRegionsErr  error
	bedrockRegionList      []string
)

func bedrockRegions() []string {
	loadBedrockRegionCatalog()
	if loadBedrockRegionsErr != nil {
		return nil
	}
	return slices.Clone(bedrockRegionList)
}

func loadBedrockRegionCatalog() {
	loadBedrockRegionsOnce.Do(func() {
		var catalog bedrockRegionsSnapshot
		if err := json.Unmarshal(bedrockRegionsRaw, &catalog); err != nil {
			loadBedrockRegionsErr = err
			return
		}
		regions := make([]string, 0, len(catalog.Regions))
		for _, region := range catalog.Regions {
			normalized := strings.TrimSpace(region) // swobu:io-string source=boundary
			if normalized == "" {
				continue
			}
			regions = append(regions, normalized)
		}
		slices.Sort(regions)
		bedrockRegionList = slices.Compact(regions)
	})
}
