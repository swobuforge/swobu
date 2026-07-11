package carrier

import (
	"encoding/json"
	"testing"
)

func mutateWireDocumentJSON(doc WireDocument, mutate func(payload map[string]any) (bool, error)) (WireDocument, bool, error) {
	var payload map[string]any
	if len(doc.Raw) != 0 {
		payload = map[string]any{}
		if err := json.Unmarshal(doc.Raw, &payload); err != nil {
			return WireDocument{}, false, err
		}
	} else {
		payload = map[string]any{}
	}
	changed, err := mutate(payload)
	if err != nil {
		return WireDocument{}, false, err
	}
	if !changed {
		return doc, false, nil
	}
	nextRaw, err := json.Marshal(payload)
	if err != nil {
		return WireDocument{}, false, err
	}
	doc.Raw = nextRaw
	return doc, true, nil
}

func TestWireDocumentMutateJSON(t *testing.T) {
	doc := WireDocument{Raw: []byte(`{"model":"m"}`)}
	next, changed, err := mutateWireDocumentJSON(doc, func(payload map[string]any) (bool, error) {
		payload["model"] = "m2"
		return true, nil
	})
	if err != nil {
		t.Fatalf("MutateJSON() error = %v", err)
	}
	if !changed {
		t.Fatal("changed=false, want true")
	}
	if string(next.Raw) != `{"model":"m2"}` {
		t.Fatalf("raw = %s", string(next.Raw))
	}
}
