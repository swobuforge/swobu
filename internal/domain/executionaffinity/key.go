package executionaffinity

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"hash"
)

// These historical domain strings are compatibility tokens. Changing them
// would remap existing routing, provider-session, and cache-locality values.
const (
	deriveDomain  = "swobu/thread-id/v1"
	projectDomain = "swobu/thread-projection/v1"
)

// Key is comparable opaque equality used only for execution placement.
type Key struct{ sum [sha256.Size]byte }

func Derive(namespace string, parts ...string) (Key, error) {
	if namespace == "" {
		return Key{}, errors.New("execution-affinity derivation namespace is empty")
	}
	digest := sha256.New()
	writeFrame(digest, deriveDomain)
	writeFrame(digest, namespace)
	for _, part := range parts {
		writeFrame(digest, part)
	}
	var key Key
	copy(key.sum[:], digest.Sum(nil))
	return key, nil
}

func Project(namespace string, key Key) (string, error) {
	if namespace == "" {
		return "", errors.New("execution-affinity projection namespace is empty")
	}
	if key.IsZero() {
		return "", errors.New("execution-affinity key is zero")
	}
	digest := sha256.New()
	writeFrame(digest, projectDomain)
	writeFrame(digest, namespace)
	writeBytesFrame(digest, key.sum[:])
	return base64.RawURLEncoding.EncodeToString(digest.Sum(nil)), nil
}

func (key Key) IsZero() bool { return key == Key{} }

func writeFrame(digest hash.Hash, value string) { writeBytesFrame(digest, []byte(value)) }
func writeBytesFrame(digest hash.Hash, value []byte) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = digest.Write(length[:])
	_, _ = digest.Write(value)
}
