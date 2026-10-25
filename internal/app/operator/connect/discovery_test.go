package connect

import (
	"context"
	"errors"
	"testing"

	"github.com/swobuforge/swobu/internal/clientconnect"
)

type interruptedDiscovery struct {
	fakeClients
	calls  int
	cancel context.CancelFunc
}

func (clients *interruptedDiscovery) Discover(context.Context, clientconnect.Target) []clientconnect.Client {
	clients.calls++
	if clients.cancel != nil {
		clients.cancel()
	}
	return []clientconnect.Client{{ID: "codex", Name: "Codex"}}
}

func TestDiscoveryCancellationCannotAcknowledgePartialInventory(t *testing.T) {
	for _, stage := range []string{"before", "during", "healthy"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			clients := &interruptedDiscovery{}
			if stage == "before" {
				cancel()
			}
			if stage == "during" {
				clients.cancel = cancel
			}
			service, err := NewService(clients, testCredentials{})
			if err != nil {
				t.Fatal(err)
			}
			target, err := clientconnect.NewTarget("research", "http://127.0.0.1:7926/workspaces/research")
			if err != nil {
				t.Fatal(err)
			}
			result, err := service.Discover(ctx, target)
			if stage == "healthy" {
				if err != nil || len(result) != 1 || clients.calls != 1 {
					t.Fatal("healthy discovery lost inventory")
				}
				return
			}
			if !errors.Is(err, context.Canceled) || result != nil {
				t.Fatal("canceled discovery acknowledged partial inventory")
			}
			if stage == "before" && clients.calls != 0 {
				t.Fatal("already canceled discovery reached filesystem owner")
			}
			if stage == "during" && clients.calls != 1 {
				t.Fatal("interrupted discovery retried")
			}
		})
	}
}
