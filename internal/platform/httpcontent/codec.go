package httpcontent

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/klauspost/compress/zstd"
)

// DecodeBytes removes supported HTTP content codings from a fully buffered body.
// It exists for request bodies where canonical interpretation needs decoded bytes first.
func DecodeBytes(contentEncoding string, raw []byte) ([]byte, error) {
	return DecodeBytesLimited(contentEncoding, raw, 0)
}

// DecodeBytesLimited removes supported HTTP content codings from a fully
// buffered body and optionally caps decoded size. A maxDecodedBytes value <= 0
// means no decoded-size cap.
func DecodeBytesLimited(contentEncoding string, raw []byte, maxDecodedBytes int64) ([]byte, error) {
	contentEncoding = normalizeContentEncoding(contentEncoding)
	if contentEncoding == "" || contentEncoding == "identity" {
		return append([]byte(nil), raw...), nil
	}

	reader, err := newDecoder(contentEncoding, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = reader.Close()
	}()

	decodedReader := io.Reader(reader)
	if maxDecodedBytes > 0 {
		decodedReader = io.LimitReader(reader, maxDecodedBytes+1)
	}
	decoded, err := io.ReadAll(decodedReader)
	if err != nil {
		return nil, fmt.Errorf("decode %s body: %w", contentEncoding, err)
	}
	if maxDecodedBytes > 0 && int64(len(decoded)) > maxDecodedBytes {
		return nil, errors.New("decoded body exceeds limit")
	}
	return decoded, nil
}

// DecodeStream removes supported HTTP content codings without forcing full-buffer
// reads, preserving incremental streaming semantics at the transport edge.
func DecodeStream(contentEncoding string, body io.ReadCloser) (io.ReadCloser, error) {
	contentEncoding = normalizeContentEncoding(contentEncoding)
	if contentEncoding == "" || contentEncoding == "identity" {
		return body, nil
	}
	return newDecoder(contentEncoding, body)
}

func normalizeContentEncoding(contentEncoding string) string {
	return strings.ToLower(strings.TrimSpace(contentEncoding))
}

func newDecoder(contentEncoding string, body io.Reader) (io.ReadCloser, error) {
	switch contentEncoding {
	case "gzip", "x-gzip":
		return gzip.NewReader(body)
	case "deflate":
		return zlib.NewReader(body)
	case "zstd":
		reader, err := zstd.NewReader(body)
		if err != nil {
			return nil, err
		}
		return zstdReadCloser{Decoder: reader}, nil
	default:
		return nil, fmt.Errorf("unsupported content encoding %q", contentEncoding)
	}
}

type zstdReadCloser struct {
	*zstd.Decoder
}

func (r zstdReadCloser) Close() error {
	r.Decoder.Close()
	return nil
}
