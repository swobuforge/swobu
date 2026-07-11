package exchange

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// ReplayBuffer captures a finite canonical event stream so fallback can replay
// request truth without re-decoding the original native payload.
type ReplayBuffer struct {
	Events []canonical.Event
}

// Replayable consumes one event reader into a replay buffer.
//
// A positive limit bounds the number of captured events. Non-positive limits
// leave the replay buffer uncapped.
func Replayable(ctx context.Context, r canonical.EventReader, limit int64) (*ReplayBuffer, error) {
	if r == nil {
		return nil, errors.New("replay event reader is required")
	}
	defer func() {
		_ = r.Close(ctx)
	}()

	events := make([]canonical.Event, 0, 8)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		ev, err := r.Next(ctx)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return &ReplayBuffer{Events: append([]canonical.Event(nil), events...)}, nil
			}
			return nil, err
		}
		events = append(events, ev)
		if limit > 0 && int64(len(events)) > limit {
			return nil, fmt.Errorf("replay buffer limit exceeded")
		}
	}
}

// Reader returns one canonical event reader over the buffered events.
func (b *ReplayBuffer) Reader() canonical.EventReader {
	if b == nil {
		return canonical.NewSliceEventReader(nil)
	}
	return canonical.NewSliceEventReader(b.Events)
}
