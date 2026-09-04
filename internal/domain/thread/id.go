package thread

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"hash"
)

const (
	deriveDomain  = "swobu/thread-id/v1"
	projectDomain = "swobu/thread-projection/v1"
)

// ID is a comparable Swobu-owned equality identity. It deliberately exposes
// no textual or byte representation; use Project only at an owning boundary.
type ID struct {
	sum [sha256.Size]byte
}

func Derive(namespace string, parts ...string) (ID, error) {
	if namespace == "" {
		return ID{}, errors.New("thread derivation namespace is empty")
	}
	digest := sha256.New()
	writeFrame(digest, deriveDomain)
	writeFrame(digest, namespace)
	for _, part := range parts {
		writeFrame(digest, part)
	}
	var id ID
	copy(id.sum[:], digest.Sum(nil))
	return id, nil
}

func Project(namespace string, id ID) (string, error) {
	if namespace == "" {
		return "", errors.New("thread projection namespace is empty")
	}
	if id.IsZero() {
		return "", errors.New("thread ID is zero")
	}
	digest := sha256.New()
	writeFrame(digest, projectDomain)
	writeFrame(digest, namespace)
	writeBytesFrame(digest, id.sum[:])
	return base64.RawURLEncoding.EncodeToString(digest.Sum(nil)), nil
}

func (id ID) IsZero() bool {
	return id == ID{}
}

func writeFrame(digest hash.Hash, value string) {
	writeBytesFrame(digest, []byte(value))
}

func writeBytesFrame(digest hash.Hash, value []byte) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = digest.Write(length[:])
	_, _ = digest.Write(value)
}
