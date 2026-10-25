package accessidentity

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

const managedPrefix = "acc_"

var Local = ID{value: "local"}

// ID is a validated, non-secret attribution identity. The zero value is
// invalid so admission boundaries cannot accidentally describe anonymity.
type ID struct{ value string }

func Parse(raw string) (ID, error) {
	switch raw {
	case Local.value:
		return Local, nil
	}
	if !strings.HasPrefix(raw, managedPrefix) {
		return ID{}, errors.New("access ID must be local or an acc_ managed ID")
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(raw, managedPrefix))
	if err != nil || len(payload) != 16 {
		return ID{}, errors.New("managed access ID must contain 16 base64url-encoded random bytes")
	}
	return ID{value: raw}, nil
}

func RestoreManaged(raw string) (ID, error) {
	id, err := Parse(raw)
	if err != nil {
		return ID{}, err
	}
	if !id.Managed() {
		return ID{}, fmt.Errorf("%q is a reserved built-in access ID", raw)
	}
	return id, nil
}

func NewManaged(random [16]byte) ID {
	return ID{value: managedPrefix + base64.RawURLEncoding.EncodeToString(random[:])}
}

func (id ID) String() string { return id.value }
func (id ID) Valid() bool    { return id.value != "" }
func (id ID) Managed() bool  { return strings.HasPrefix(id.value, managedPrefix) }
