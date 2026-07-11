package exchange

import (
	"context"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func projectClientDocument(ctx context.Context, envelope canonical.EventReader) (canonical.CanonicalOutput, error) {
	closed, err := canonical.ReadClosedEnvelope(ctx, envelope, canonical.EnvResponse)
	_ = envelope.Close(ctx)
	if err != nil {
		return nil, err
	}
	return closed.ProjectResponse()
}
