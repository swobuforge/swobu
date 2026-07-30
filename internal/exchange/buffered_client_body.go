package exchange

import (
	"bytes"
	"context"
	"io"
	"sync"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/historyfingerprint"
	"github.com/swobuforge/swobu/internal/wire"
)

// bufferedClientBody defers canonical consumption and checkpoint commit until the
// inbound delivery owner reads the response body. Exchange selects the client
// codec, but it does not consume the response stream during reduction.
type bufferedClientBody struct {
	ctx        context.Context
	call       providerCall
	envelope   canonical.ResponseStream
	initialize sync.Once
	close      sync.Once
	reader     *bytes.Reader
	err        error
	consumed   bool
	completion *wire.ResponseCompletion
	complete   func(*historyfingerprint.Response, []compat.Change)
	fail       func(error)
}

func newBufferedClientBody(ctx context.Context, call providerCall, envelope canonical.ResponseStream) *bufferedClientBody {
	completion, complete, fail := wire.NewResponseCompletion()
	return &bufferedClientBody{
		ctx: ctx, call: call, envelope: envelope,
		completion: completion, complete: complete, fail: fail,
	}
}

func (b *bufferedClientBody) Read(p []byte) (int, error) {
	b.initialize.Do(b.prepare)
	if b.err != nil {
		return 0, b.err
	}
	return b.reader.Read(p)
}

func (b *bufferedClientBody) prepare() {
	response, err := projectClientDocument(b.ctx, b.envelope)
	b.consumed = true
	if err != nil {
		b.fail(err)
		if terminal, ok := b.envelope.(interface{ TerminalError() error }); ok && terminal.TerminalError() != nil {
			b.err = terminal.TerminalError()
			return
		}
		b.err = err
		return
	}
	document, err := b.call.clientCodec.EncodeResponseDocument(b.call.fullRequest, response)
	if err != nil {
		b.fail(err)
		b.err = err
		return
	}
	b.complete(document.ResponseFingerprint, document.Changes)
	b.reader = bytes.NewReader(document.Document.RawBytes())
}

func (b *bufferedClientBody) Close() error {
	var err error
	b.close.Do(func() {
		if !b.consumed && b.envelope != nil {
			b.fail(io.ErrClosedPipe)
			err = b.envelope.Close(b.ctx)
		}
	})
	return err
}

func (b *bufferedClientBody) TerminalError() error { return b.err }

var _ io.ReadCloser = (*bufferedClientBody)(nil)
