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
	ctx      context.Context
	events   canonical.ResponseStream
	encode   ResponseEventEncoder
	pending  []byte
	terminal error
	close    sync.Once
	closeErr error
}

func NewEncodedResponseBody(ctx context.Context, events canonical.ResponseStream, encode ResponseEventEncoder) *EncodedResponseBody {
	return &EncodedResponseBody{ctx: ctx, events: events, encode: encode}
}

func (b *EncodedResponseBody) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for len(b.pending) == 0 {
		if b.ctx == nil || b.events == nil || b.encode == nil {
			return 0, errors.New("encoded response body is incomplete")
		}
		if err := b.ctx.Err(); err != nil {
			return 0, err
		}
		event, err := b.events.Next(b.ctx)
		if err != nil {
			if errors.Is(err, io.EOF) {
				b.terminal = responseStreamTerminalError(b.events)
			}
			return 0, err
		}
		encoded, err := b.encode(event)
		if err != nil {
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
		if b.events != nil {
			b.closeErr = b.events.Close(b.ctx)
		}
	})
	return b.closeErr
}

// TerminalError reports a replay or provider terminal failure discovered only
// after the encoded body was consumed.
func (b *EncodedResponseBody) TerminalError() error { return b.terminal }

func responseStreamTerminalError(events canonical.ResponseStream) error {
	type terminalErrorSource interface{ CommitError() error }
	if source, ok := events.(terminalErrorSource); ok {
		return source.CommitError()
	}
	return nil
}
