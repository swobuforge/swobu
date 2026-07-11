package exchange

import (
	"context"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func projectClientDocument(ctx context.Context, envelope canonical.EventReader) (Result[canonical.CanonicalOutput], error) {
	link := NewLink(
		LinkID("client.response.snapshot"),
		semanticEventsPort(),
		clientOutputPort(),
		func(ctx context.Context, envelope canonical.EventReader) (Result[canonical.CanonicalOutput], error) {
			closed, err := canonical.ReadClosedEnvelope(ctx, envelope, canonical.EnvResponse)
			_ = envelope.Close(ctx)
			if err != nil {
				return Result[canonical.CanonicalOutput]{}, err
			}
			output, err := closed.ProjectResponse()
			if err != nil {
				return Result[canonical.CanonicalOutput]{}, err
			}
			return NewResult[canonical.CanonicalOutput](output), nil
		},
	)
	return link.Run(ctx, envelope)
}
