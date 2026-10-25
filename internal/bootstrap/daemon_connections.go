package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	credentials "github.com/swobuforge/swobu/internal/adapters/outbound/credentials"
	connectionapp "github.com/swobuforge/swobu/internal/app/operator/connections"
	"github.com/swobuforge/swobu/internal/configstore"
	"github.com/swobuforge/swobu/internal/connections"
	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
	"github.com/swobuforge/swobu/internal/nodeauthority"
	"github.com/swobuforge/swobu/internal/nodefile"
	"github.com/swobuforge/swobu/internal/profile"
)

// openDaemonConnections opens reusable state once per daemon lifetime. Existing
// Models and Connections both live in the same canonical YAML. Connection
// runtime snapshots retain their typed codec but publish through the same
// atomic file boundary as Workspace routing, never a second JSON sidecar.
func openDaemonConnections(ctx context.Context, logger *slog.Logger, configPath string, workspaces *configstore.Store) (*connectionapp.Queries, *connections.Store, *connections.Lifecycle, *nodefile.File, error) {
	openNode := nodefile.Open
	if workspaces.ReadOnly() {
		openNode = nodefile.OpenReadOnly
	}
	file, err := openNode(configPath)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	// Bootstrap opens Workspace routing and the common Node file separately.
	// Externally managed YAML may change between those reads; refuse a mixed
	// snapshot rather than serving policies from different file generations.
	_, fileRevision, err := file.Snapshot()
	if err != nil {
		return nil, nil, nil, nil, err
	}
	if fileRevision != workspaces.OpenedNodeRevision() {
		return nil, nil, nil, nil, errors.New("Node configuration changed between Workspace and Node startup snapshots; restart with one stable revision")
	}
	if err := file.InstallAdmission(nodeauthority.Admit); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("validate complete Node authority: %w", err)
	}
	raw, err := file.ConnectionDocument()
	if err != nil {
		return nil, nil, nil, nil, err
	}
	registry, err := connections.OpenInline(raw, file.PublishConnections, profile.SupportsSpec)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	if !workspaces.ReadOnly() {
		if err := workspaces.InstallNodePublisher(file.PublishRouting); err != nil {
			return nil, nil, nil, nil, err
		}
	}
	lifecycle, err := connections.NewLifecycle(registry, workspaces)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	// Maintenance failure never rewrites application outcome or prevents serving
	// valid configuration. The durable metadata/marker permits a later retry.
	queries, err := connectionapp.NewQueries(registry, lifecycle)
	return queries, registry, lifecycle, file, err
}

// Connection maintenance retires only Connection-owned staged material.
func retireDaemonConnectionCredentialForConfig(_ string) func(context.Context, connectionidentity.ID, credentialref.Ref) error {
	return func(ctx context.Context, id connectionidentity.ID, ref credentialref.Ref) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if ref.String() == "" {
			return errors.New("empty Connection credential reference")
		}
		return credentials.RetireConnectionCredential(id.String(), ref.String())
	}
}
