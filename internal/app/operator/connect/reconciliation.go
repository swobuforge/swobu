package connect

import (
	"context"
	"errors"
	"slices"

	"github.com/swobuforge/swobu/internal/clientconnect"
)

// Reconciliation is a fresh observation against original reviewed intent.
// Configured does not attribute a write to Swobu; unchanged does not prove that
// no effect occurred. Neither observation authorizes or retries an effect.
type Reconciliation struct {
	State string `json:"state"`
	Plan  Plan   `json:"plan"`
}

// Reconcile inspects current client configuration without dispatching Apply.
// It validates the original review binding before inspection and retains the
// original intent rather than refreshing consent after an uncertain response.
func (service *Service) Reconcile(ctx context.Context, rawClient string, reviewed Review, target clientconnect.Target) (Reconciliation, error) {
	if err := ctx.Err(); err != nil {
		return Reconciliation{}, err
	}
	if err := target.ValidateLocal(); err != nil {
		return Reconciliation{}, err
	}
	clientID, err := clientconnect.ParseClientID(rawClient)
	if err != nil {
		return Reconciliation{}, err
	}
	if reviewed.Revision == "" {
		return Reconciliation{}, ErrReviewRequired
	}
	if reviewed.Authentication.Principal != target.Principal() || reviewed.ConfigPaths == nil || reviewed.Changes == nil || reviewed.Revision != reviewRevision(target, clientID, reviewed) {
		return Reconciliation{}, errors.New("invalid original client configuration review")
	}
	current, err := service.Plan(ctx, rawClient, target)
	if err != nil {
		return Reconciliation{}, err
	}
	state := "changed"
	if current.Revision == reviewed.Revision && reviewMatches(reviewed, current) {
		state = "unchanged"
	}
	if current.AlreadyConfigured && slices.Equal(reviewed.ConfigPaths, current.ConfigPaths) {
		state = "configured"
	}
	return Reconciliation{State: state, Plan: current}, nil
}
