package carrier

import "testing"

func TestWireDocumentDecodeJSON(t *testing.T) {
	doc := WireDocument{Raw: []byte(`{"model":"m","x":1}`)}
	payload, err := DecodeWireDocumentJSON(doc)
	if err != nil {
		t.Fatalf("DecodeJSON() error = %v", err)
	}
	if payload["model"] != "m" {
		t.Fatalf("model = %v, want m", payload["model"])
	}
}

func TestWireDocumentMutateJSON(t *testing.T) {
	doc := WireDocument{Raw: []byte(`{"model":"m"}`)}
	next, changed, err := MutateWireDocumentJSON(doc, func(payload map[string]any) (bool, error) {
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
