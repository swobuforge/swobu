package modelcatalog

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/swobuforge/swobu/internal/domain/compatibility"
	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	"github.com/swobuforge/swobu/internal/domain/routetarget"
	"github.com/swobuforge/swobu/internal/ports"
)

// Snapshot is the operator read model returned by the app layer for daemon and
// TUI surfaces. It reflects selected-target catalog truth without pushing
// backend transport or provider DTOs into operator code.
type Snapshot struct {
	Entries []Entry `json:"entries"`
}

// Entry is one selected-provider catalog projection for one endpoint. Errors
// stay local to the entry so one failed backend does not hide the rest of the
// operator surface.
type Entry struct {
	EndpointName      string   `json:"endpoint_name"`
	ProviderConfigRef string   `json:"provider_config_ref"`
	ProviderSpec      string   `json:"provider_spec"`
	ProtocolKind      string   `json:"protocol_kind"`
	ModelIDs          []string `json:"model_ids,omitempty"`
	Error             string   `json:"error,omitempty"`
}

// Loader owns the operator-support model catalog read path. It loads durable
// endpoint intent, resolves each selected provider target, and asks provider
// wiring to fetch one catalog per endpoint.
type Loader struct {
	endpoints ports.EndpointLister
	providers ports.ProviderModelCatalog
	coord     *loadCoordinator
}

type loadCoordinator struct {
	mu           sync.Mutex
	cancelActive context.CancelFunc
	activeID     uint64
}

var modelCatalogProbeTimeout = 8 * time.Second

func NewLoader(endpoints ports.EndpointLister, providers ports.ProviderModelCatalog) Loader {
	return Loader{
		endpoints: endpoints,
		providers: providers,
		coord:     &loadCoordinator{},
	}
}

func (l Loader) Load(ctx context.Context) (Snapshot, error) {
	if l.endpoints == nil {
		return Snapshot{}, errInternalCatalog("endpoint lister is not configured")
	}
	if l.providers == nil {
		return Snapshot{}, errInternalCatalog("provider model catalog is not configured")
	}

	loadCtx, release := l.beginLoad(ctx)
	defer release()

	endpoints, err := l.endpoints.ListEndpoints(loadCtx)
	if err != nil {
		return Snapshot{}, errInternalCatalog("endpoint catalog could not be loaded")
	}
	slices.SortFunc(endpoints, func(a, b endpointintent.Endpoint) int {
		return compareStrings(a.Name().String(), b.Name().String())
	})

	entries := make([]Entry, 0, len(endpoints))
	for _, endpoint := range endpoints {
		resolved, err := routetarget.ResolveRoutableTarget(endpoint)
		if err != nil {
			entries = append(entries, Entry{
				EndpointName: endpoint.Name().String(),
				Error:        err.Error(),
			})
			continue
		}
		selected := resolved.ProviderConfig
		entry := Entry{
			EndpointName:      endpoint.Name().String(),
			ProviderConfigRef: selected.Ref().String(),
			ProviderSpec:      selected.ProviderSpec().String(),
			ProtocolKind:      selected.ProtocolKind().String(),
		}
		probeCtx, cancelProbe := context.WithTimeout(loadCtx, modelCatalogProbeTimeout)
		models, probeErr := probeRouteModels(probeCtx, l.providers, modelCatalogProbeInput{
			ProviderConfigRef: selected.Ref().String(),
			ProviderSpec:      selected.ProviderSpec().String(),
			BaseURL:           selected.BaseURL(),
			CredentialRef:     selected.CredentialRef(),
			ProtocolKind:      selected.ProtocolKind(),
		})
		cancelProbe()
		entry.ModelIDs = models
		entry.Error = formatLoadProbeError(probeErr)
		entries = append(entries, entry)
	}

	return Snapshot{Entries: entries}, nil
}

func (l Loader) beginLoad(parent context.Context) (context.Context, func()) {
	if l.coord == nil {
		ctx, cancel := context.WithCancel(parent)
		return ctx, cancel
	}
	ctx, cancel := context.WithCancel(parent)
	l.coord.mu.Lock()
	l.coord.activeID++
	id := l.coord.activeID
	prev := l.coord.cancelActive
	l.coord.cancelActive = cancel
	l.coord.mu.Unlock()
	if prev != nil {
		prev()
	}
	return ctx, func() {
		l.coord.mu.Lock()
		if l.coord.activeID == id {
			l.coord.cancelActive = nil
		}
		l.coord.mu.Unlock()
		cancel()
	}
}

func formatLoadProbeError(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, context.Canceled):
		return "model catalog refresh superseded by a newer request"
	case errors.Is(err, context.DeadlineExceeded):
		return "model catalog probe timed out"
	default:
		return err.Error()
	}
}

func errInternalCatalog(message string) error {
	return compatibility.InternalError(message)
}

func compareStrings(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}
