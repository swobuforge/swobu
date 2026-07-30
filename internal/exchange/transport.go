package exchange

import (
	"bytes"
	"io"
	"net/http"
	"strings"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/wire"
)

// ClientResponse is the sealed client-delivery sum. Its concrete variants
// make neither/both pointer states unrepresentable.
type ClientResponse interface{ isClientResponse() }

type BufferedResponse struct {
	Response   carrier.Response
	completion *wire.ResponseCompletion
}

type StreamingResponse struct {
	Response   carrier.Response
	completion *wire.ResponseCompletion
}

type MessageStreamingResponse struct {
	Response   carrier.MessageTransportResponse
	completion *wire.ResponseCompletion
}

func (BufferedResponse) isClientResponse()         {}
func (StreamingResponse) isClientResponse()        {}
func (MessageStreamingResponse) isClientResponse() {}

func NewBufferedResponse(doc carrier.Document) ClientResponse {
	header := cloneHeader(doc.Header)
	if header.Get("Content-Type") == "" {
		header.Set("Content-Type", "application/json")
	}
	return BufferedResponse{Response: carrier.Response{
		Status: http.StatusOK,
		Header: header,
		Body:   io.NopCloser(bytes.NewReader(doc.RawBytes())),
	}}
}

func newBufferedClientResponse(body *bufferedClientBody) ClientResponse {
	return BufferedResponse{Response: carrier.Response{
		Status: http.StatusOK,
		Header: http.Header{"Content-Type": []string{"application/json"}},
		Body:   body,
	}, completion: body.completion}
}

func NewStreamingResponse(stream carrier.ByteStream, completion *wire.ResponseCompletion) ClientResponse {
	header := cloneHeader(stream.Header)
	if header.Get("Content-Type") == "" {
		if strings.TrimSpace(stream.MediaType) == "" { // swobu:io-string source=boundary
			header.Set("Content-Type", "application/octet-stream")
		} else {
			header.Set("Content-Type", stream.MediaType)
		}
	}
	body := stream.Body
	if body == nil {
		body = io.NopCloser(bytes.NewReader(nil))
	}
	return StreamingResponse{Response: carrier.Response{
		Status: http.StatusOK,
		Header: header,
		Body:   body,
	}, completion: completion}
}

func NewMessageStreamingResponse(stream carrier.MessageResponse, completion *wire.ResponseCompletion) ClientResponse {
	header := cloneHeader(stream.Header)
	if header.Get("Content-Type") == "" {
		header.Set("Content-Type", stream.MediaType)
	}
	return MessageStreamingResponse{Response: carrier.MessageTransportResponse{
		Status:   http.StatusOK,
		Header:   header,
		Messages: stream.Messages,
	}, completion: completion}
}

func responseCompletion(response ClientResponse) *wire.ResponseCompletion {
	switch value := response.(type) {
	case BufferedResponse:
		return value.completion
	case StreamingResponse:
		return value.completion
	case MessageStreamingResponse:
		return value.completion
	default:
		return nil
	}
}

func cloneHeader(in http.Header) http.Header {
	if in == nil {
		return make(http.Header)
	}
	out := make(http.Header, len(in))
	for k, values := range in {
		copied := make([]string, len(values))
		copy(copied, values)
		out[k] = copied
	}
	return out
}

func deliveryIsIncremental(clientDelivery delivery.Delivery, providerDelivery delivery.Delivery) bool {
	return clientDelivery.IsStreaming() && providerDelivery.IsStreaming()
}
