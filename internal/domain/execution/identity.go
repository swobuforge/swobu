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

func (id InvocationID) String() string {
	if !id.Valid() {
		return ""
	}
	return "inv_" + strings.TrimPrefix(id.exchange.String(), exchangePrefix) + "_" + strconv.FormatUint(id.ordinal, 10)
}

func (id InvocationID) Valid() bool            { return id.exchange.Valid() && id.ordinal != 0 }
func (id InvocationID) ExchangeID() ExchangeID { return id.exchange }
func (id InvocationID) Ordinal() uint64        { return id.ordinal }
