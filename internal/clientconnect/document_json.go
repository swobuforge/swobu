package clientconnect

import (
	"encoding/json"
	"fmt"

	"github.com/tailscale/hujson"
)

type jsonEditor struct{ allowComments bool }

func (e jsonEditor) parse(raw []byte) (hujson.Value, error) {
	doc, err := hujson.Parse(raw)
	if err != nil {
		return hujson.Value{}, fmt.Errorf("document is not valid JSON")
	}
	if !e.allowComments && !doc.IsStandard() {
		return hujson.Value{}, fmt.Errorf("document is not strict JSON")
	}
	return doc, nil
}

func (e jsonEditor) String(raw []byte, path keyPath) (string, bool, error) {
	doc, err := e.parse(raw)
	if err != nil {
		return "", false, err
	}
	node, exists, err := jsonPath(&doc, path, false)
	if err != nil || !exists {
		return "", exists, err
	}
	literal, ok := node.Value.(hujson.Literal)
	if !ok {
		return "", false, fmt.Errorf("%s is not a string", joinKeyPath(path))
	}
	var value string
	if err := json.Unmarshal(literal, &value); err != nil {
		return "", false, fmt.Errorf("%s is not a string", joinKeyPath(path))
	}
	return value, true, nil
}

func (e jsonEditor) Value(raw []byte, path keyPath, out any) (bool, error) {
	doc, err := e.parse(raw)
	if err != nil {
		return false, err
	}
	node, exists, err := jsonPath(&doc, path, false)
	if err != nil || !exists {
		return exists, err
	}
	packed := node.Clone().Pack()
	if err := json.Unmarshal(packed, out); err != nil {
		return false, fmt.Errorf("%s has incompatible structure", joinKeyPath(path))
	}
	return true, nil
}

func (e jsonEditor) SetString(raw []byte, path keyPath, value string) ([]byte, error) {
	if _, _, err := e.String(raw, path); err != nil {
		return nil, err
	}
	encoded, _ := json.Marshal(value)
	return e.SetValue(raw, path, encoded)
}

// SetValue patches one JSON value while preserving all unrelated source bytes.
func (e jsonEditor) SetValue(raw []byte, path keyPath, encoded []byte) ([]byte, error) {
	doc, err := e.parse(raw)
	if err != nil {
		return nil, err
	}
	node, existed, err := jsonPath(&doc, path, true)
	if err != nil {
		return nil, err
	}
	_ = existed
	valueDoc, err := hujson.Parse(encoded)
	if err != nil || !valueDoc.IsStandard() {
		return nil, fmt.Errorf("replacement for %s is not valid JSON", joinKeyPath(path))
	}
	node.Value = valueDoc.Value
	out := doc.Pack()
	return out, nil
}

// Delete removes one exact object member and leaves absent paths unchanged.
func (e jsonEditor) Delete(raw []byte, path keyPath) ([]byte, error) {
	doc, err := e.parse(raw)
	if err != nil {
		return nil, err
	}
	if len(path) == 0 {
		return nil, fmt.Errorf("empty key path")
	}
	parent, exists, err := jsonPath(&doc, path[:len(path)-1], false)
	if err != nil || !exists {
		return raw, err
	}
	object, ok := parent.Value.(*hujson.Object)
	if !ok {
		return nil, fmt.Errorf("%s is not an object", joinKeyPath(path[:len(path)-1]))
	}
	for i := range object.Members {
		name, _ := jsonString(object.Members[i].Name)
		if name == path[len(path)-1] {
			object.Members = append(object.Members[:i], object.Members[i+1:]...)
			return doc.Pack(), nil
		}
	}
	return raw, nil
}

func jsonPath(root *hujson.Value, path keyPath, create bool) (*hujson.Value, bool, error) {
	current := root
	for i, segment := range path {
		object, ok := current.Value.(*hujson.Object)
		if !ok {
			return nil, false, fmt.Errorf("%s is not an object", joinKeyPath(path[:i]))
		}
		var found *hujson.Value
		for j := range object.Members {
			name, ok := jsonString(object.Members[j].Name)
			if !ok {
				return nil, false, fmt.Errorf("object key is not a string")
			}
			if name == segment {
				if found != nil {
					return nil, false, fmt.Errorf("%s is ambiguous", joinKeyPath(path[:i+1]))
				}
				found = &object.Members[j].Value
			}
		}
		if found == nil {
			if !create {
				return nil, false, nil
			}
			var child hujson.Value
			if i == len(path)-1 {
				child.Value = hujson.String("")
			} else {
				child.Value = &hujson.Object{}
			}
			object.Members = append(object.Members, hujson.ObjectMember{Name: hujson.Value{Value: hujson.String(segment)}, Value: child})
			found = &object.Members[len(object.Members)-1].Value
		}
		if i == len(path)-1 {
			return found, true, nil
		}
		current = found
	}
	return nil, false, fmt.Errorf("empty key path")
}

func jsonString(value hujson.Value) (string, bool) {
	literal, ok := value.Value.(hujson.Literal)
	if !ok {
		return "", false
	}
	var out string
	if json.Unmarshal(literal, &out) != nil {
		return "", false
	}
	return out, true
}

func joinKeyPath(path keyPath) string {
	if len(path) == 0 {
		return "document root"
	}
	out := path[0]
	for _, part := range path[1:] {
		out += "." + part
	}
	return out
}
