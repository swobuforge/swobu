package chatcompletions

import (
	"context"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

type testClientRequestDecoder struct{}

type recordingChanges = []compat.Change

func (testClientRequestDecoder) DecodeClientRequest(doc carrier.Document) (canonical.CanonicalRequest, delivery.Delivery, error) {
	result, err := (ClientRequestDecoder{}).DecodeClientRequest(doc)
	if err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	return result.Request.Request, result.Request.Delivery, nil
}

type testResponseStreamEncoder struct{}

func (testResponseStreamEncoder) EncodeResponseStream(ctx context.Context, events canonical.ResponseStream, d delivery.Delivery) (carrier.ByteStream, error) {
	result, err := (ResponseStreamEncoder{}).EncodeResponseStream(ctx, canonical.CanonicalRequest{}, events, d)
	return result.Stream, err
}
