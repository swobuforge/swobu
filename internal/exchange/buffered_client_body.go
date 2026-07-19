package exchange

import (
	"bytes"
	"context"
	"io"
	"sync"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// bufferedClientBody defers canonical consumption and replay commit until the
// inbound delivery owner reads the response body. Exchange selects the client
// codec, but it does not consume the response stream during reduction.
type bufferedClientBody struct {
	ctx        context.Context
	call       preparedProviderCall
	envelope   canonical.ResponseStream
	sink       compat.Sink
	initialize sync.Once
	close      sync.Once
	reader     *bytes.Reader
	err        error
	consumed   bool
}

func newBufferedClientBody(ctx context.Context, call preparedProviderCall, envelope canonical.ResponseStream, sink compat.Sink) *bufferedClientBody {
	return &bufferedClientBody{ctx: ctx, call: call, envelope: envelope, sink: sink}
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
		b.err = err
		return
	}
	if commitAware, ok := b.envelope.(interface{ CommitError() error }); ok {
		if err := commitAware.CommitError(); err != nil {
			b.err = err
			return
		}
	}
	document, err := b.call.clientCodec.EncodeResponseDocument(response)
	commitDecisionsBestEffort(b.ctx, b.sink, b.call.exchangeID, document.Decisions)
	if err != nil {
		b.err = err
		return
	}
	b.reader = bytes.NewReader(document.Document.RawBytes())
}

func (b *bufferedClientBody) Close() error {
	var err error
	b.close.Do(func() {
		if !b.consumed && b.envelope != nil {
			err = b.envelope.Close(b.ctx)
		}
	})
	return err
}

func (b *bufferedClientBody) TerminalError() error { return b.err }

var _ io.ReadCloser = (*bufferedClientBody)(nil)
