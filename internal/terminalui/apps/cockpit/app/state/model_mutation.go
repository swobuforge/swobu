package state

import (
	"slices"
	"strings"
)

func applyWorkspaceRename(model *Model, current, next string) {
	if current == "" || next == "" || current == next {
		return
	}
	for i, name := range model.Endpoints {
		if name != current {
			continue
		}
		model.Endpoints[i] = next
	}
	slices.Sort(model.Endpoints)
	for i := range model.Catalog {
		if model.Catalog[i].EndpointName == current {
			model.Catalog[i].EndpointName = next
		}
	}
	for i := range model.EndpointSnapshots {
		if model.EndpointSnapshots[i].Name == current {
			model.EndpointSnapshots[i].Name = next
		}
	}
	if model.CurrentEndpoint == current {
		model.CurrentEndpoint = next
	}
}

func applyWorkspaceCreate(model *Model, name string) {
	if name == "" {
		return
	}
	if !containsString(model.Endpoints, name) {
		model.Endpoints = append(model.Endpoints, name)
		slices.Sort(model.Endpoints)
	}
	model.CurrentEndpoint = name
}

func applyWorkspaceDelete(model *Model, name string) {
	name = strings.TrimSpace(name) // trimlowerlint:allow boundary canonicalization
	if name == "" {
		return
	}
	filteredEndpoints := make([]string, 0, len(model.Endpoints))
	for _, endpoint := range model.Endpoints {
		if strings.TrimSpace(endpoint) == name { // trimlowerlint:allow boundary canonicalization
			continue
		}
		filteredEndpoints = append(filteredEndpoints, endpoint)
	}
	model.Endpoints = filteredEndpoints

	filteredCatalog := make([]CatalogEntry, 0, len(model.Catalog))
	for _, entry := range model.Catalog {
		if strings.TrimSpace(entry.EndpointName) == name { // trimlowerlint:allow boundary canonicalization
			continue
		}
		filteredCatalog = append(filteredCatalog, entry)
	}
	model.Catalog = filteredCatalog

	filteredSnapshots := make([]EndpointSnapshot, 0, len(model.EndpointSnapshots))
	for _, snapshot := range model.EndpointSnapshots {
		if strings.TrimSpace(snapshot.Name) == name { // trimlowerlint:allow boundary canonicalization
			continue
		}
		filteredSnapshots = append(filteredSnapshots, snapshot)
	}
	model.EndpointSnapshots = filteredSnapshots

	if strings.TrimSpace(model.CurrentEndpoint) == name { // trimlowerlint:allow boundary canonicalization
		model.CurrentEndpoint = firstOrEmpty(model.Endpoints)
	}
}

func applyRoutingSelection(model *Model, endpointName, providerRef string) {
	for i := range model.EndpointSnapshots {
		if model.EndpointSnapshots[i].Name != endpointName {
			continue
		}
		model.EndpointSnapshots[i].SelectedProviderConfigRef = providerRef
	}
	for i := range model.Catalog {
		if model.Catalog[i].EndpointName != endpointName {
			continue
		}
		model.Catalog[i].ProviderConfigRef = providerRef
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func firstOrEmpty(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func endpointNames(entries []CatalogEntry) []string {
	names := make([]string, 0, len(entries))
	seen := map[string]struct{}{}
	for _, entry := range entries {
		name := strings.TrimSpace(entry.EndpointName) // trimlowerlint:allow boundary canonicalization
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func endpointSnapshotNames(entries []EndpointSnapshot) []string {
	names := make([]string, 0, len(entries))
	seen := map[string]struct{}{}
	for _, entry := range entries {
		name := strings.TrimSpace(entry.Name) // trimlowerlint:allow boundary canonicalization
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func cloneCatalogEntries(entries []CatalogEntry) []CatalogEntry {
	out := make([]CatalogEntry, 0, len(entries))
	for _, entry := range entries {
		entry.ModelIDs = append([]string(nil), entry.ModelIDs...)
		out = append(out, entry)
	}
	return out
}

func cloneEndpointSnapshots(entries []EndpointSnapshot) []EndpointSnapshot {
	out := make([]EndpointSnapshot, 0, len(entries))
	for _, entry := range entries {
		entry.ProviderConfigs = append([]ProviderConfigSnapshot(nil), entry.ProviderConfigs...)
		out = append(out, entry)
	}
	return out
}

func cloneTrafficRows(entries []TrafficRow) []TrafficRow {
	return append([]TrafficRow(nil), entries...)
}
