package ports

import (
	"context"
	"slices"
	"strings"
)

// ProviderModelCatalog reads operator-support model catalogs for one selected
// provider target. It is separate from compatibility-path semantic execution.
type ProviderModelCatalog interface {
	ListModels(ctx context.Context, target RoutableTarget) ([]string, error)
}

// CloneModelIDs protects operator read models from accidental mutation by
// callers or transport renderers.
func CloneModelIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id) // trimlowerlint:allow boundary canonicalization
		if id == "" {
			continue
		}
		out = append(out, id)
	}
	return slices.Clone(out)
}
