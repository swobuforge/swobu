package canonical

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
)

// JSONObject is the sole deterministic representation of object-semantic
// JSON in the canonical graph. Object keys are recursively ordered while
// array order, strings, and number lexemes retain their input meaning.
type JSONObject struct {
	canonical []byte
}

// ParseJSONObject validates exactly one JSON object, rejects duplicate keys at
// every depth, and returns its deterministic compact representation.
func ParseJSONObject(raw []byte) (JSONObject, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	token, err := decoder.Token()
	if err != nil {
		return JSONObject{}, fmt.Errorf("parse JSON object: %w", err)
	}
	delim, ok := token.(json.Delim)
	if !ok || delim != '{' {
		return JSONObject{}, fmt.Errorf("parse JSON object: top-level value is not an object")
	}
	canonical, err := canonicalizeJSONObject(decoder)
	if err != nil {
		return JSONObject{}, err
	}
	if token, err = decoder.Token(); err != io.EOF {
		if err != nil {
			return JSONObject{}, fmt.Errorf("parse JSON object trailing data: %w", err)
		}
		return JSONObject{}, fmt.Errorf("parse JSON object: trailing value %v", token)
	}
	return JSONObject{canonical: canonical}, nil
}

// EmptyJSONObject returns the canonical empty object.
func EmptyJSONObject() JSONObject {
	return JSONObject{canonical: []byte("{}")}
}

// Bytes returns a copy of the deterministic object bytes.
func (o JSONObject) Bytes() []byte {
	return append([]byte(nil), o.canonical...)
}

// String returns the deterministic compact object representation.
func (o JSONObject) String() string {
	return string(o.canonical)
}

// IsEmpty reports whether the value is the zero value or the empty object.
func (o JSONObject) IsEmpty() bool {
	return len(o.canonical) == 0 || bytes.Equal(o.canonical, []byte("{}"))
}

// Clone returns an independent copy of the canonical bytes.
func (o JSONObject) Clone() JSONObject {
	return JSONObject{canonical: o.Bytes()}
}

type canonicalJSONMemberData struct {
	key   string
	value []byte
}

func canonicalizeJSONObject(decoder *json.Decoder) ([]byte, error) {
	members := make([]canonicalJSONMemberData, 0)
	seen := make(map[string]struct{})
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return nil, fmt.Errorf("parse JSON object key: %w", err)
		}
		key, ok := keyToken.(string)
		if !ok {
			return nil, fmt.Errorf("parse JSON object: non-string key")
		}
		if _, duplicate := seen[key]; duplicate {
			return nil, fmt.Errorf("parse JSON object: duplicate key %q", key)
		}
		seen[key] = struct{}{}
		valueToken, err := decoder.Token()
		if err != nil {
			return nil, fmt.Errorf("parse JSON object value for %q: %w", key, err)
		}
		value, err := canonicalizeJSONValue(decoder, valueToken)
		if err != nil {
			return nil, err
		}
		members = append(members, canonicalJSONMemberData{key: key, value: value})
	}
	if token, err := decoder.Token(); err != nil {
		return nil, fmt.Errorf("parse JSON object end: %w", err)
	} else if token != json.Delim('}') {
		return nil, fmt.Errorf("parse JSON object: invalid closing token %v", token)
	}
	sort.Slice(members, func(i, j int) bool { return members[i].key < members[j].key })
	var out bytes.Buffer
	out.WriteByte('{')
	for index, member := range members {
		if index > 0 {
			out.WriteByte(',')
		}
		key, _ := json.Marshal(member.key)
		out.Write(key)
		out.WriteByte(':')
		out.Write(member.value)
	}
	out.WriteByte('}')
	return out.Bytes(), nil
}

func canonicalizeJSONValue(decoder *json.Decoder, token json.Token) ([]byte, error) {
	switch value := token.(type) {
	case json.Delim:
		switch value {
		case '{':
			return canonicalizeJSONObject(decoder)
		case '[':
			return canonicalizeJSONArray(decoder)
		default:
			return nil, fmt.Errorf("parse JSON value: unexpected delimiter %q", value)
		}
	case json.Number:
		// Decoder validation makes the original number token safe to retain. Not
		// coercing through float64 prevents schema bounds and tool inputs changing.
		return []byte(value.String()), nil
	case string, bool, nil:
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("encode JSON value: %w", err)
		}
		return encoded, nil
	default:
		return nil, fmt.Errorf("parse JSON value: unsupported token %T", token)
	}
}

func canonicalizeJSONArray(decoder *json.Decoder) ([]byte, error) {
	var out bytes.Buffer
	out.WriteByte('[')
	index := 0
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, fmt.Errorf("parse JSON array value: %w", err)
		}
		value, err := canonicalizeJSONValue(decoder, token)
		if err != nil {
			return nil, err
		}
		if index > 0 {
			out.WriteByte(',')
		}
		out.Write(value)
		index++
	}
	if token, err := decoder.Token(); err != nil {
		return nil, fmt.Errorf("parse JSON array end: %w", err)
	} else if token != json.Delim(']') {
		return nil, fmt.Errorf("parse JSON array: invalid closing token %v", token)
	}
	out.WriteByte(']')
	return out.Bytes(), nil
}
