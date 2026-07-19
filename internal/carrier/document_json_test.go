package carrier

import (
	"encoding/json"
	"testing"
)

func mutateDocumentJSON(doc Document, mutate func(payload map[string]any) (bool, error)) (Document, bool, error) {
	var payload map[string]any
	if len(doc.Raw) != 0 {
		payload = map[string]any{}
		if err := json.Unmarshal(doc.Raw, &payload); err != nil {
			return Document{}, false, err
		}
	} else {
		payload = map[string]any{}
	}
	changed, err := mutate(payload)
	if err != nil {
		return Document{}, false, err
	}
	if !changed {
		return doc, false, nil
	}
	nextRaw, err := json.Marshal(payload)
	if err != nil {
		return Document{}, false, err
	}
	doc.Raw = nextRaw
	return doc, true, nil
}

func TestDocumentMutateJSON(t *testing.T) {
	doc := Document{Raw: []byte(`{"model":"m"}`)}
	next, changed, err := mutateDocumentJSON(doc, func(payload map[string]any) (bool, error) {
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
