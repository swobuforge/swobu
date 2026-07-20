package exchange

import (
	"context"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func projectClientDocument(ctx context.Context, envelope canonical.ResponseStream) (canonical.CanonicalResponse, error) {
	closed, err := canonical.ReadClosedEnvelope(ctx, envelope, canonical.EnvResponse)
	_ = envelope.Close(ctx)
	if err != nil {
		return canonical.CanonicalResponse{}, err
	}
	output, err := closed.ProjectResponse()
	if err != nil {
		return canonical.CanonicalResponse{}, err
	}
	return *output, nil
}
