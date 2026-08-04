package historyfingerprint

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"hash"
	"io"
	"sort"
)

const (
	requestDomain  = "swobu/history-request"
	responseDomain = "swobu/history-response"
	historyDomain  = "swobu/history"
)

// Scheme versions one client codec's private protocol-history representation.
type Scheme string

// History identifies one ordered sequence of completed client-visible
// request/response contributions.
type History struct {
	scheme Scheme
	sum    [sha256.Size]byte
}

// Request identifies one client-visible request contribution.
type Request struct {
	scheme Scheme
	sum    [sha256.Size]byte
}

// Response identifies one unambiguous client-visible response contribution.
type Response struct {
	scheme Scheme
	sum    [sha256.Size]byte
}

// FrameJSONValue converts one JSON value into deterministic semantic material.
// Object member order and insignificant whitespace do not affect the result;
// JSON kinds, array order, object keys, and number spellings remain distinct.
func FrameJSONValue(raw []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if err := requireJSONEnd(decoder); err != nil {
		return nil, err
	}
	return appendJSONValue(nil, value), nil
}

// Scheme returns the codec-local representation scheme.
func (h History) Scheme() Scheme { return h.scheme }

// Scheme returns the codec-local representation scheme.
func (r Request) Scheme() Scheme { return r.scheme }

// Scheme returns the codec-local representation scheme.
func (r Response) Scheme() Scheme { return r.scheme }

// FingerprintRequest hashes protocol-native request material under the request
// leaf domain. Empty schemes are rejected.
func FingerprintRequest(scheme Scheme, material []byte) (Request, error) {
	if scheme == "" {
		return Request{}, errors.New("history fingerprint scheme is empty")
	}
	return Request{scheme: scheme, sum: leafSum(requestDomain, scheme, material)}, nil
}

// FingerprintResponse hashes protocol-native response material under the
// response leaf domain. Empty schemes are rejected.
func FingerprintResponse(scheme Scheme, material []byte) (Response, error) {
	if scheme == "" {
		return Response{}, errors.New("history fingerprint scheme is empty")
	}
	return Response{scheme: scheme, sum: leafSum(responseDomain, scheme, material)}, nil
}

// Advance composes one predecessor, request leaf, and response leaf in that
// exact order. Every component must be valid and use the same scheme.
func Advance(previous *History, request Request, response Response) (History, error) {
	if request.scheme == "" || response.scheme == "" ||
		request.sum == ([sha256.Size]byte{}) || response.sum == ([sha256.Size]byte{}) {
		return History{}, errors.New("history fingerprint leaf is invalid")
	}
	if request.scheme != response.scheme {
		return History{}, errors.New("history fingerprint schemes do not match")
	}
	if previous != nil {
		if previous.scheme == "" || previous.sum == ([sha256.Size]byte{}) {
			return History{}, errors.New("previous history fingerprint is invalid")
		}
		if previous.scheme != request.scheme {
			return History{}, errors.New("previous history fingerprint scheme does not match")
		}
	}

	h := sha256.New()
	writeFrame(h, []byte(historyDomain))
	writeFrame(h, []byte(request.scheme))
	if previous == nil {
		writeFrame(h, []byte{0})
	} else {
		writeFrame(h, []byte{1})
		writeFrame(h, previous.sum[:])
	}
	writeFrame(h, request.sum[:])
	writeFrame(h, response.sum[:])
	var sum [sha256.Size]byte
	copy(sum[:], h.Sum(nil))
	return History{scheme: request.scheme, sum: sum}, nil
}

func leafSum(domain string, scheme Scheme, material []byte) [sha256.Size]byte {
	h := sha256.New()
	writeFrame(h, []byte(domain))
	writeFrame(h, []byte(scheme))
	writeFrame(h, material)
	var sum [sha256.Size]byte
	copy(sum[:], h.Sum(nil))
	return sum
}

func writeFrame(h hash.Hash, value []byte) {
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(value)))
	_, _ = h.Write(size[:])
	_, _ = h.Write(value)
}

func requireJSONEnd(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("history fingerprint JSON contains multiple values")
		}
		return err
	}
	return nil
}

func appendJSONValue(dst []byte, value any) []byte {
	switch typed := value.(type) {
	case nil:
		return appendByteFrame(dst, 'n')
	case bool:
		if typed {
			return appendByteFrame(dst, 't')
		}
		return appendByteFrame(dst, 'f')
	case string:
		dst = appendByteFrame(dst, 's')
		return appendSliceFrame(dst, []byte(typed))
	case json.Number:
		dst = appendByteFrame(dst, '#')
		return appendSliceFrame(dst, []byte(typed.String()))
	case []any:
		dst = appendByteFrame(dst, '[')
		dst = appendSize(dst, len(typed))
		for _, item := range typed {
			dst = appendJSONValue(dst, item)
		}
		return dst
	case map[string]any:
		dst = appendByteFrame(dst, '{')
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		dst = appendSize(dst, len(keys))
		for _, key := range keys {
			dst = appendSliceFrame(dst, []byte(key))
			dst = appendJSONValue(dst, typed[key])
		}
		return dst
	default:
		panic("history fingerprint received an unsupported decoded JSON value")
	}
}

func appendByteFrame(dst []byte, value byte) []byte {
	return appendSliceFrame(dst, []byte{value})
}

func appendSliceFrame(dst, value []byte) []byte {
	dst = appendSize(dst, len(value))
	return append(dst, value...)
}

func appendSize(dst []byte, size int) []byte {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], uint64(size))
	return append(dst, encoded[:]...)
}
