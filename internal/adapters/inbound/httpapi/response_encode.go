package httpapi

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/compatibility"
	"github.com/swobuforge/swobu/internal/ports"
)

func writeSuccessResponse(w http.ResponseWriter, family compatibility.IngressFamily, resp ports.ExecuteResponse) error {
	switch resp.DeliveryMode() {
	case compatibility.DeliveryModeBuffered:
		return writeBufferedSuccess(w, family, resp.Output())
	case compatibility.DeliveryModeStreaming:
		return writeStreamingSuccess(w, family, resp.Stream())
	default:
		return compatibility.UnsupportedDelivery("response delivery mode is not implemented")
	}
}

// writeBufferedSuccess delegates profile-specific success encoding to the
// client-family codec selected at the HTTP edge.
func writeBufferedSuccess(w http.ResponseWriter, family compatibility.IngressFamily, output compatibility.CanonicalOutput) error {
	if output == nil {
		return compatibility.InternalError("buffered provider response is missing a canonical output")
	}

	codec, err := codecForFamily(family)
	if err != nil {
		return err
	}
	body, err := codec.encodeBuffered(output)
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
func writeStreamingSuccess(w http.ResponseWriter, family compatibility.IngressFamily, stream compatibility.CanonicalOutputEventStream) error {
	if stream == nil {
		return compatibility.InternalError("streaming provider response is missing an output event stream")
	}

	codec, err := codecForFamily(family)
	if err != nil {
		return err
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	flusher, _ := w.(http.Flusher)
	state := codec.newStreamState()
	for {
		event, err := stream.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return compatibility.InternalError("stream decoding failed")
		}
		frames, err := state.Encode(event)
		if err != nil {
			return err
		}
		for _, frame := range frames {
			if _, err := w.Write(frame); err != nil {
				return err
			}
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
	tail, err := state.Finish()
	if err != nil {
		return err
	}
	for _, frame := range tail {
		if _, err := w.Write(frame); err != nil {
			return err
		}
	}
	if flusher != nil {
		flusher.Flush()
	}
	return nil
}

func outputText(items []compatibility.OutputItem) string {
	out := ""
	for _, item := range items {
		if item.Kind != compatibility.OutputItemText {
			continue
		}
		out += item.Text
	}
	return out
}

func containsToolUseOutput(items []compatibility.OutputItem) bool {
	for _, item := range items {
		if item.Kind == compatibility.OutputItemToolUse {
			return true
		}
	}
	return false
}

func defaultFinishReason(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func fallbackID(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

type clientStreamEncoder interface {
	Encode(event compatibility.OutputEvent) ([][]byte, error)
	Finish() ([][]byte, error)
}

func sseData(raw []byte) []byte {
	return append(append([]byte("data: "), raw...), []byte("\n\n")...)
}

func sseEventFrame(event string, raw []byte) []byte {
	frame := make([]byte, 0, len(event)+len(raw)+16)
	frame = append(frame, []byte("event: ")...)
	frame = append(frame, event...)
	frame = append(frame, '\n')
	frame = append(frame, []byte("data: ")...)
	frame = append(frame, raw...)
	frame = append(frame, '\n', '\n')
	return frame
}
