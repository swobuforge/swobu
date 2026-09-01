// Package shareprotocol defines the minimal public interoperability law between
// a Swobu Owner daemon and a Swobu Relay. It is not a Relay SDK.
package shareprotocol

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base32"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	Version                = 1
	RelayHostname          = "relay.share.swobu.com"
	MaxControlMessageBytes = 256 << 10
)

type Message struct {
	Version             int      `json:"version"`
	Type                string   `json:"type"`
	CSR                 string   `json:"csr,omitempty"`
	PriorChain          []string `json:"prior_chain,omitempty"`
	CertificateChain    []string `json:"certificate_chain,omitempty"`
	ChallengePrivateKey string   `json:"challenge_private_key,omitempty"`
	Error               string   `json:"error,omitempty"`
}

type Codec struct {
	reader *bufio.Reader
	writer io.Writer
}

func NewCodec(stream io.ReadWriter) *Codec {
	return &Codec{reader: bufio.NewReaderSize(stream, MaxControlMessageBytes+1), writer: stream}
}

func (c *Codec) Write(message Message) error {
	message.Version = Version
	raw, err := json.Marshal(message)
	if err != nil {
		return err
	}
	if len(raw) > MaxControlMessageBytes {
		return errors.New("share control message too large")
	}
	_, err = c.writer.Write(append(raw, '\n'))
	return err
}

func (c *Codec) Read() (Message, error) {
	raw, err := c.reader.ReadSlice('\n')
	if errors.Is(err, bufio.ErrBufferFull) {
		return Message{}, errors.New("share control message too large")
	}
	if err != nil {
		return Message{}, err
	}
	if len(raw) > MaxControlMessageBytes {
		return Message{}, errors.New("share control message too large")
	}
	var message Message
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&message); err != nil {
		return Message{}, err
	}
	if message.Version != Version {
		return Message{}, fmt.Errorf("unsupported share protocol version %d", message.Version)
	}
	return message, nil
}

func EndpointID(publicKey any) (string, error) {
	der, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return "", fmt.Errorf("marshal endpoint public key: %w", err)
	}
	digest := sha256.Sum256(der)
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(digest[:16])), nil
}

func Hostname(endpointID string) string { return "d-" + endpointID + ".share.swobu.com" }
