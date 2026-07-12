package canonical

import "strings"

// RawJSON stores one raw JSON object payload.
//
// It is used for schema-like request fields that must stay opaque until a
// protocol adapter or canonical validator inspects them.
type RawJSON struct {
	rawObject string
}

// NewRawJSONObject trims one JSON object payload into canonical form.
func NewRawJSONObject(raw string) RawJSON {
	return RawJSON{rawObject: strings.TrimSpace(raw)} // swobu:io-string source=domain
}

func (r RawJSON) RawObject() string {
	return r.rawObject
}

func (r RawJSON) IsEmpty() bool {
	return strings.TrimSpace(r.rawObject) == "" // swobu:io-string source=domain
}

func (r RawJSON) Clone() RawJSON {
	return RawJSON{rawObject: r.rawObject}
}
