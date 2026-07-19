package canonical

import (
	"context"
)

type errorEventReader struct {
	err error
}

func NewErrorEventReader(err error) ResponseStream {
	return &errorEventReader{err: err}
}

func (r *errorEventReader) Next(context.Context) (Event, error) {
	return Event{}, r.err
}

func (r *errorEventReader) Close(context.Context) error {
	return nil
}
