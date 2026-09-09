package cli

import (
	"context"
	"fmt"
	"net/http"

	"github.com/swobuforge/swobu/internal/app/operator/daemonlifecycle"
)

func requireCompatibleControlPlane(ctx context.Context, client *http.Client, addr string) error {
	payload, class := daemonlifecycle.FetchStatus(ctx, client, addr)
	if class == daemonlifecycle.StatusClassDown {
		return fmt.Errorf("daemon is not reachable; run `swobu status`")
	}
	return daemonlifecycle.RequireControlPlaneProtocol(payload)
}
