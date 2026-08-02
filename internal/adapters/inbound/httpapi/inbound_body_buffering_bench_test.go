package httpapi

import (
	"bytes"
	"io"
	"net/http"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

// BenchmarkInboundBodyBuffering measures the per-request allocation cost of the
// inbound request-body journey on the daemon hot path:
//
//	wire → io.ReadAll (decodeRequestBody)
//	     → httpcontent identity clone (DecodeBytesLimited)
//	     → newTransportRequest (adopts bytes, no copy)
//	     → exchange byte-owned transport boundary
//	     → carrier.NewDocument boundary clone
//
// The live alloc profile (2026-08-01, /tmp/allocs_post.pb.gz) showed this path
// owning ~190 MB before task 030: io.ReadAll #2 at 150 MB (split 75/75
// inbound-vs-exchange — the exchange half being a redundant re-materialization
// of bytes the inbound edge had already buffered), plus the newTransportRequest
// (15 MB) clone of a body dead one statement later. 030 makes the inbound edge
// transfer ownership of the materialized bytes so exchange adopts them in place
// instead of io.ReadAll-ing a second copy. This benchmark is the before/after
// evidence.
func BenchmarkInboundBodyBuffering(b *testing.B) {
	for _, bodySize := range []int{2 << 10, 32 << 10, 256 << 10} {
		wireBody := bytes.Repeat([]byte("a"), bodySize)
		b.Run(bodySizeName(bodySize), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(bodySize))
			for i := 0; i < b.N; i++ {
				// 1. Inbound edge: read + identity-decode the body.
				req := mustHTTPRequest(b, wireBody)
				decoded, err := decodeRequestBody(nilWriter{}, req)
				if err != nil {
					b.Fatal(err)
				}
				// 2. Wrap into a transport request for the exchange boundary.
				// newTransportRequest now transfers ownership of `decoded`.
				transport := newTransportRequest(http.MethodPost, "/v1/messages", req.Header, decoded)
				// 3. Exchange-side document construction adopts the byte-owned body.
				if _, err := exchangeDocument(transport); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func bodySizeName(size int) string {
	switch size {
	case 2 << 10:
		return "2KB"
	case 32 << 10:
		return "32KB"
	default:
		return "256KB"
	}
}

// exchangeDocument mirrors exchange.newClientRequestDocument's byte-owned body
// boundary and final carrier.Document detach.
func exchangeDocument(transport carrier.TransportRequest) (carrier.Document, error) {
	if len(transport.Body) > 48<<20 {
		return carrier.Document{}, io.ErrShortBuffer
	}
	return carrier.NewDocument(
		protocolkind.Messages,
		"application/json",
		transport.Header,
		transport.Body,
		carrier.Meta{},
	), nil
}

func mustHTTPRequest(tb testing.TB, body []byte) *http.Request {
	tb.Helper()
	req, err := http.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	if err != nil {
		tb.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	return req
}

type nilWriter struct{}

func (nilWriter) Header() http.Header       { return http.Header{} }
func (nilWriter) Write([]byte) (int, error) { return 0, io.EOF }
func (nilWriter) WriteHeader(int)           {}
