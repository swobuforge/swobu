// Package cockpit is the interactive operator-surface launch seam.
//
// The subtree below it is intentionally split into:
//   - `engine/` for the extractable retained runtime framework
//   - `toolkit/` for generic batteries-included views
//   - `app/` for the Swobu cockpit itself
package cockpit

import (
	"context"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"

	operatorclient "github.com/swobuforge/swobu/internal/app/operator/client"
	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	platformconfig "github.com/swobuforge/swobu/internal/platform/config"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	stateeffect "github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state/effect"
	rootviews "github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/views/root"
	"github.com/swobuforge/swobu/internal/terminalui/apps/shared/daemonstate"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/host"
)

// Run is the interactive cockpit entry seam.
func Run(ctx context.Context, stdin io.Reader, stdout io.Writer, _ io.Writer) error {
	_ = stdin
	_ = stdout
	screen, err := tcell.NewScreen()
	if err != nil {
		return err
	}
	model := bootstrapModelFromDaemon(ctx)
	runner := host.New(screen, rootviews.Root(), model, state.Reduce)
	restoreForegroundRunner := stateeffect.SetForegroundClientRunner(func(ctx context.Context, executable string, args []string, env map[string]string) (int, error) {
		exitCode, err := host.RunForegroundClient(ctx, executable, args, env)
		if err == nil {
			return exitCode, nil
		}
		if errors.Is(err, host.ErrForegroundHandoffActive) {
			return 0, stateeffect.ErrForegroundClientActive
		}
		if errors.Is(err, host.ErrForegroundHandoffUnavailable) {
			return 0, stateeffect.ErrForegroundClientUnavailable
		}
		return 0, err
	})
	defer restoreForegroundRunner()
	return runner.Run(ctx)
}

func bootstrapModelFromDaemon(ctx context.Context) state.Model {
	model := state.Model{
		HeaderStatus:  daemonstate.HeaderOfflineStale,
		DaemonState:   daemonstate.DaemonStateUnreachable,
		StreamEnabled: true,
	}
	client := operatorclient.New(&http.Client{Timeout: 800 * time.Millisecond}, platformconfig.DefaultDaemonURL())
	endpoints, err := client.List(ctx)
	if err != nil || len(endpoints) == 0 {
		return model
	}
	model.HeaderStatus = daemonstate.HeaderReady
	model.DaemonState = daemonstate.DaemonStateUp
	model.EndpointSnapshots = make([]state.EndpointSnapshot, 0, len(endpoints))
	for _, ep := range endpoints {
		snapshot := endpointSnapshotFromIntent(ep)
		if strings.TrimSpace(snapshot.Name) == "" { // trimlowerlint:allow boundary canonicalization
			continue
		}
		model.EndpointSnapshots = append(model.EndpointSnapshots, snapshot)
	}
	model.Endpoints = endpointSnapshotNames(model.EndpointSnapshots)
	if len(model.Endpoints) > 0 {
		model.CurrentEndpoint = model.Endpoints[0]
		model.FooterShowTabs = true
	}
	return model
}

func endpointSnapshotFromIntent(ep endpointintent.Endpoint) state.EndpointSnapshot {
	snapshot := state.EndpointSnapshot{
		Name:                      strings.TrimSpace(ep.Name().String()),                      // trimlowerlint:allow boundary canonicalization
		SelectedProviderConfigRef: strings.TrimSpace(ep.SelectedProviderConfigRef().String()), // trimlowerlint:allow boundary canonicalization
		ProviderConfigs:           make([]state.ProviderConfigSnapshot, 0, len(ep.ProviderConfigs())),
	}
	for _, providerConfig := range ep.ProviderConfigs() {
		snapshot.ProviderConfigs = append(snapshot.ProviderConfigs, state.ProviderConfigSnapshot{
			Ref:           strings.TrimSpace(providerConfig.Ref().String()),          // trimlowerlint:allow boundary canonicalization
			ProviderSpec:  strings.TrimSpace(providerConfig.ProviderSpec().String()), // trimlowerlint:allow boundary canonicalization
			BaseURL:       strings.TrimSpace(providerConfig.BaseURL()),               // trimlowerlint:allow boundary canonicalization
			CredentialRef: strings.TrimSpace(providerConfig.CredentialRef()),         // trimlowerlint:allow boundary canonicalization
			ModelID:       strings.TrimSpace(providerConfig.ModelID()),               // trimlowerlint:allow boundary canonicalization
			TargetAlias:   strings.TrimSpace(providerConfig.TargetAlias()),           // trimlowerlint:allow boundary canonicalization
		})
	}
	return snapshot
}

func endpointSnapshotNames(entries []state.EndpointSnapshot) []string {
	seen := make(map[string]struct{}, len(entries))
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := strings.TrimSpace(entry.Name) // trimlowerlint:allow boundary canonicalization
		if name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}
