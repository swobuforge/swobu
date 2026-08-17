package wire

import (
	"context"
	"errors"
	"io"
	"sync"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// EncodedResponseMessages preserves the messages emitted by a client codec.
// Each successful Next returns exactly one intended wire message, even when a
// canonical event encodes to zero or several protocol messages.
type EncodedResponseMessages struct {
	events     canonical.ResponseStream
	encode     ResponseEventEncoder
	completion *ResponseCompletion
	fail       func(error)
	pending    [][]byte
	close      sync.Once
	closeErr   error
}

func NewEncodedResponseMessages(events canonical.ResponseStream, encode ResponseEventEncoder, completion *ResponseCompletion, fail func(error)) *EncodedResponseMessages {
	return &EncodedResponseMessages{events: events, encode: encode, completion: completion, fail: fail}
}

func (s *EncodedResponseMessages) Next(ctx context.Context) ([]byte, error) {
	for len(s.pending) == 0 {
		if s.events == nil || s.encode == nil {
			err := errors.New("encoded response message stream is incomplete")
			s.fail(err)
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			s.fail(err)
			return nil, err
		}
		event, err := s.events.Next(ctx)
		if err != nil {
			if errors.Is(err, io.EOF) && s.completion.Snapshot().State == CompletionPending {
				s.fail(io.ErrUnexpectedEOF)
			}
			return nil, err
		}
		if event.Kind == canonical.EventUsage {
			if payload, ok := event.Payload.(canonical.UsagePayload); ok {
				s.completion.ObserveUsage(payload.Usage)
			}
		}
		messages, err := s.encode(event)
		if err != nil {
			s.fail(err)
			return nil, err
		}
		for _, message := range messages {
			s.pending = append(s.pending, append([]byte(nil), message...))
		}
	}
	message := s.pending[0]
	s.pending = s.pending[1:]
	return message, nil
}

func (s *EncodedResponseMessages) Close(ctx context.Context) error {
	s.close.Do(func() {
		if s.completion.Snapshot().State == CompletionPending {
			s.fail(io.ErrClosedPipe)
		}
		if s.events != nil {
			s.closeErr = s.events.Close(ctx)
		}
	})
	return s.closeErr
}
