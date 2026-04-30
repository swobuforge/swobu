package ports

import (
	"context"

	"github.com/metrofun/swobu/internal/domain/endpointintent"
	stateModel "github.com/metrofun/swobu/internal/terminalui/apps/cockpit/app/state/model"
)

// DaemonControlPlane is the narrow daemon-facing seam used by cockpit effects.
type DaemonControlPlane interface {
	ListEndpoints(ctx context.Context) ([]endpointintent.Endpoint, error)
	GetEndpoint(ctx context.Context, name string) (endpointintent.Endpoint, error)
	PutEndpoint(ctx context.Context, ep endpointintent.Endpoint) (endpointintent.Endpoint, error)
	DeleteEndpoint(ctx context.Context, name string) error
}

// Clipboard is the narrow clipboard seam used by cockpit effects.
type Clipboard interface {
	CopyValue(text string) string
}

// ClientLauncher is the narrow client-launch seam used by cockpit effects.
type ClientLauncher interface {
	Launch(baseURL string) string
}

// StatusReader is the narrow daemon read-model seam for cockpit refresh effects.
type StatusReader interface {
	LoadDaemonStatus(ctx context.Context) (state string, endpointCount int, err error)
	LoadCatalog(ctx context.Context) ([]stateModel.CatalogEntry, error)
	LoadStatusProjection(ctx context.Context) ([]stateModel.TrafficRow, error)
}
