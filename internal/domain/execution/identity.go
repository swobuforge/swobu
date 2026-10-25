package execution

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const exchangePrefix = "ex_"

type ExchangeID struct{ value string }

func NewExchangeID() (ExchangeID, error) { return newExchangeID(rand.Reader) }

func newExchangeID(entropy io.Reader) (ExchangeID, error) {
	var random [16]byte
	if _, err := io.ReadFull(entropy, random[:]); err != nil {
		return ExchangeID{}, fmt.Errorf("allocate exchange identity: %w", err)
	}
	return ExchangeID{value: exchangePrefix + base64.RawURLEncoding.EncodeToString(random[:])}, nil
}

// ParseExchangeID restores an Exchange identity from a persisted canonical
// string. It accepts only identities produced by NewExchangeID.
func ParseExchangeID(raw string) (ExchangeID, error) {
	if !strings.HasPrefix(raw, exchangePrefix) {
		return ExchangeID{}, errors.New("exchange identity has an invalid prefix")
	}
	encoded := strings.TrimPrefix(raw, exchangePrefix)
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(decoded) != 16 {
		return ExchangeID{}, errors.New("exchange identity has invalid entropy")
	}
	return ExchangeID{value: raw}, nil
}

func (id ExchangeID) String() string { return id.value }
func (id ExchangeID) Valid() bool    { return id.value != "" }

type InvocationID struct {
	exchange ExchangeID
	ordinal  uint64
}

func NewInvocationID(exchange ExchangeID, ordinal uint64) (InvocationID, error) {
	if !exchange.Valid() || ordinal == 0 {
		return InvocationID{}, errors.New("invocation identity requires exchange and ordinal")
	}
	return InvocationID{exchange: exchange, ordinal: ordinal}, nil
}

// ParseInvocationID restores an invocation identity and verifies that its
// embedded Exchange component is independently canonical.
func ParseInvocationID(raw string) (InvocationID, error) {
	if !strings.HasPrefix(raw, "inv_") {
		return InvocationID{}, errors.New("invocation identity has an invalid prefix")
	}
	prefix := strings.TrimPrefix(raw, "inv_")
	separator := strings.LastIndexByte(prefix, '_')
	if separator <= 0 || separator == len(prefix)-1 {
		return InvocationID{}, errors.New("invocation identity has invalid shape")
	}
	exchange, err := ParseExchangeID(exchangePrefix + prefix[:separator])
	if err != nil {
		return InvocationID{}, err
	}
	ordinal, err := strconv.ParseUint(prefix[separator+1:], 10, 64)
	if err != nil || ordinal == 0 {
		return InvocationID{}, errors.New("invocation identity has invalid ordinal")
	}
	return NewInvocationID(exchange, ordinal)
}

func (id InvocationID) String() string {
	if !id.Valid() {
		return ""
	}
	return "inv_" + strings.TrimPrefix(id.exchange.String(), exchangePrefix) + "_" + strconv.FormatUint(id.ordinal, 10)
}

func (id InvocationID) Valid() bool            { return id.exchange.Valid() && id.ordinal != 0 }
func (id InvocationID) ExchangeID() ExchangeID { return id.exchange }
func (id InvocationID) Ordinal() uint64        { return id.ordinal }
