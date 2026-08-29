package exchange

import (
	"context"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/wire"
)

// stagedResponseStream assigns an error stage at the catch boundary without
// replacing an earlier stage carried through a composed stream.
type stagedResponseStream struct {
	upstream canonical.ResponseStream
	stage    string
}

func newStagedResponseStream(upstream canonical.ResponseStream, stage string) *stagedResponseStream {
	return &stagedResponseStream{upstream: upstream, stage: stage}
}

func (s *stagedResponseStream) Next(ctx context.Context) (canonical.Event, error) {
	event, err := s.upstream.Next(ctx)
	return event, wire.StageResponseFailure(s.stage, err)
}

func (s *stagedResponseStream) Close(ctx context.Context) error {
	return wire.StageResponseFailure(s.stage, s.upstream.Close(ctx))
}

var _ canonical.ResponseStream = (*stagedResponseStream)(nil)
