package carrier

import "encoding/json"

// DecodeWireDocumentJSON decodes one wire-document JSON payload.
func DecodeWireDocumentJSON(doc WireDocument) (map[string]any, error) {
	payload := map[string]any{}
	if err := json.Unmarshal(doc.Raw, &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// MutateWireDocumentJSON decodes one wire-document JSON payload, applies one mutation
// function, and re-encodes only when the mutation reports changes.
func MutateWireDocumentJSON(doc WireDocument, mutate func(payload map[string]any) (changed bool, err error)) (WireDocument, bool, error) {
	if len(doc.Raw) == 0 {
		return doc, false, nil
	}
	payload, err := DecodeWireDocumentJSON(doc)
	if err != nil {
		return WireDocument{}, false, err
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
