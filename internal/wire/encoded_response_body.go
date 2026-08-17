package wire

import (
	"context"
	"errors"
	"io"
	"sync"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// ResponseEventEncoder lowers one canonical response event into zero or more
// client-wire byte sequences.
type ResponseEventEncoder func(canonical.Event) ([][]byte, error)

// EncodedResponseBody is a pull-based client response body. The inbound
// delivery owner drives canonical consumption through Read; no forwarding
// goroutine competes for stream ownership. Close propagates exactly once.
type EncodedResponseBody struct {
	ctx        context.Context
	events     canonical.ResponseStream
	encode     ResponseEventEncoder
	completion *ResponseCompletion
	fail       func(error)
	pending    []byte
	terminal   error
	close      sync.Once
	closeErr   error
}

func NewEncodedResponseBody(ctx context.Context, events canonical.ResponseStream, encode ResponseEventEncoder, completion *ResponseCompletion, fail func(error)) *EncodedResponseBody {
	return &EncodedResponseBody{ctx: ctx, events: events, encode: encode, completion: completion, fail: fail}
}

func (b *EncodedResponseBody) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for len(b.pending) == 0 {
		if b.ctx == nil || b.events == nil || b.encode == nil {
			err := errors.New("encoded response body is incomplete")
			b.fail(err)
			return 0, err
		}
		if err := b.ctx.Err(); err != nil {
			b.fail(err)
			return 0, err
		}
		event, err := b.events.Next(b.ctx)
		if err != nil {
			if errors.Is(err, io.EOF) && b.completion.Snapshot().State == CompletionPending {
				b.fail(io.ErrUnexpectedEOF)
			}
			return 0, err
		}
		if event.Kind == canonical.EventUsage {
			if payload, ok := event.Payload.(canonical.UsagePayload); ok {
				b.completion.ObserveUsage(payload.Usage)
			}
		}
		encoded, err := b.encode(event)
		if err != nil {
			b.fail(err)
			return 0, err
		}
		for _, bytes := range encoded {
			b.pending = append(b.pending, bytes...)
		}
	}
	n := copy(p, b.pending)
	b.pending = b.pending[n:]
	return n, nil
}

func (b *EncodedResponseBody) Close() error {
	b.close.Do(func() {
		if b.completion.Snapshot().State == CompletionPending {
			b.fail(io.ErrClosedPipe)
		}
		if b.events != nil {
			b.closeErr = b.events.Close(b.ctx)
		}
	})
	return b.closeErr
}

// TerminalError reports a terminal failure discovered only after the encoded
// body was consumed.
func (b *EncodedResponseBody) TerminalError() error { return b.terminal }
