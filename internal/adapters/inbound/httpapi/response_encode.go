package httpapi

import (
	"context"
	"errors"
	"github.com/swobuforge/swobu/internal/adapters/protocolsurface"
	"io"
	"net/http"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/ports"
)

func writeSuccessResponse(w http.ResponseWriter, family canonical.IngressFamily, resp ports.ProviderResponse, streaming bool) error {
	if !streaming {
		envelope := resp.EnvelopeStream()
		if envelope == nil {
			return canonical.InternalError("buffered provider response is missing a canonical envelope stream")
		}
		output, err := projectBufferedOutputFromEnvelope(envelope)
		if err != nil {
			return err
		}
		return writeBufferedSuccess(w, family, output)
	}
	envelope := resp.EnvelopeStream()
	if envelope == nil {
		return canonical.InternalError("streaming provider response is missing a canonical envelope stream")
	}
	return writeStreamingSuccess(w, family, envelope)
}

func projectBufferedOutputFromEnvelope(envelope canonical.EventReader) (canonical.CanonicalOutput, error) {
	closed, err := canonical.ReadClosedEnvelope(context.Background(), envelope, canonical.EnvResponse)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, canonical.InternalError("buffered provider response envelope ended before response closure")
		}
		return nil, canonical.InternalError("buffered provider response envelope could not be read")
	}
	output, err := closed.ProjectResponse()
	if err != nil {
		return nil, canonical.InternalError("buffered provider response envelope could not be projected")
	}
	return output, nil
}

// writeBufferedSuccess delegates profile-specific success encoding to the
// client-family codec selected at the HTTP edge.
func writeBufferedSuccess(w http.ResponseWriter, family canonical.IngressFamily, output canonical.CanonicalOutput) error {
	if output == nil {
		return canonical.InternalError("buffered provider response is missing a canonical output")
	}

	codec, err := protocolsurface.ForIngressFamily(family)
	if err != nil {
		return err
	}
	body, err := codec.EncodeBuffered(output)
	if err != nil {
		return err
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, err = w.Write(body)
	return err
}

// writeStreamingSuccess delegates stream-frame encoding to the selected
// client-family codec so provider SSE shapes never leak through this boundary.
// family-specific codecs and terminal transport conditions at one choke point.
func writeStreamingSuccess(w http.ResponseWriter, family canonical.IngressFamily, envelope canonical.EventReader) error {
	if envelope == nil {
		return canonical.InternalError("streaming provider response is missing an envelope event stream")
	}

	codec, err := protocolsurface.ForIngressFamily(family)
	if err != nil {
		return err
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	sink := httpFrameSink{w: w}
	if err := drainEncodedFrames(context.Background(), envelope, codec.NewStreamState(), sink); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return canonical.InternalError("stream decoding failed")
	}
	return nil
}

type httpFrameSink struct {
	w http.ResponseWriter
}

func (s httpFrameSink) WriteFrame(frame []byte) error {
	_, err := s.w.Write(frame)
	return err
}

func (s httpFrameSink) Flush() error {
	flusher, _ := s.w.(http.Flusher)
	if flusher != nil {
		flusher.Flush()
	}
	return nil
}
