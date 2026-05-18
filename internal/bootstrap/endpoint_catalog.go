package bootstrap

import (
	"context"
	"io/fs"
	"slices"
	"sort"
	"sync"

	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	"github.com/swobuforge/swobu/internal/platform/config"
)

type endpointCatalog struct {
	mu         sync.RWMutex
	configPath string
	runtime    config.RuntimeConfig
	byName     map[string]endpointintent.Endpoint
	all        []endpointintent.Endpoint
}

func newEndpointCatalog(configPath string, runtime config.RuntimeConfig, endpoints []endpointintent.Endpoint) *endpointCatalog {
	byName := make(map[string]endpointintent.Endpoint, len(endpoints))
	for _, endpoint := range endpoints {
		byName[endpoint.Name().String()] = endpoint
	}
	return &endpointCatalog{
		configPath: configPath,
		runtime:    runtime,
		byName:     byName,
		all:        slices.Clone(endpoints),
	}
}

func (c *endpointCatalog) GetEndpoint(_ context.Context, name endpointintent.EndpointName) (endpointintent.Endpoint, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	endpoint, ok := c.byName[name.String()]
	if !ok {
		return endpointintent.Endpoint{}, fs.ErrNotExist
	}
	return endpoint, nil
}

func (c *endpointCatalog) ListEndpoints(context.Context) ([]endpointintent.Endpoint, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return slices.Clone(c.all), nil
}

func (c *endpointCatalog) SaveEndpoints(_ context.Context, endpoints []endpointintent.Endpoint) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	sorted := slices.Clone(endpoints)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Name().String() < sorted[j].Name().String()
	})
	if err := config.Save(c.configPath, c.runtime, sorted); err != nil {
		return err
	}
	byName := make(map[string]endpointintent.Endpoint, len(sorted))
	for _, endpoint := range sorted {
		byName[endpoint.Name().String()] = endpoint
	}
	c.byName = byName
	c.all = sorted
	return nil
}

func (c *endpointCatalog) Count() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.all)
}
