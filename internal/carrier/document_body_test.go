package carrier

import (
	"encoding/json"
	"testing"
)

type testBodyAuthority string

const (
	testBodyAuthorityRaw  testBodyAuthority = "raw"
	testBodyAuthorityJSON testBodyAuthority = "json"
)

type testDocumentBody struct {
	raw       []byte
	json      map[string]any
	authority testBodyAuthority
	dirty     bool
}

func newTestDocumentBodyFromRaw(raw []byte) testDocumentBody {
	return testDocumentBody{
		raw:       append([]byte(nil), raw...),
		authority: testBodyAuthorityRaw,
	}
}

func (b *testDocumentBody) JSON() (map[string]any, error) {
	if b.json != nil {
		return b.json, nil
	}
	payload := map[string]any{}
	if len(b.raw) == 0 {
		b.json = payload
		return b.json, nil
	}
	if err := json.Unmarshal(b.raw, &payload); err != nil {
		return nil, err
	}
	b.json = payload
	return b.json, nil
}

func (b *testDocumentBody) Raw() ([]byte, error) {
	if b.authority == testBodyAuthorityRaw {
		return append([]byte(nil), b.raw...), nil
	}
	if !b.dirty {
		return append([]byte(nil), b.raw...), nil
	}
	nextRaw, err := json.Marshal(b.json)
	if err != nil {
		return nil, err
	}
	b.raw = nextRaw
	b.dirty = false
	return append([]byte(nil), b.raw...), nil
}

func (b *testDocumentBody) IsDirty() bool { return b.dirty }

func mutateTestDocumentBodyJSON(body *testDocumentBody, fn func(map[string]any) (bool, error)) (bool, error) {
	if body == nil {
		return false, nil
	}
	payload, err := body.JSON()
	if err != nil {
		return false, err
	}
	changed, err := fn(payload)
	if err != nil {
		return false, err
	}
	if !changed {
		return false, nil
	}
	body.authority = testBodyAuthorityJSON
	body.dirty = true
	return true, nil
}

func TestDocumentBody_AuthorityAndDirtyLifecycle(t *testing.T) {
	body := newTestDocumentBodyFromRaw([]byte(`{"a":1}`))

	payload, err := body.JSON()
	if err != nil {
		t.Fatalf("JSON() error = %v", err)
	}
	if payload["a"] != float64(1) {
		t.Fatalf("unexpected payload: %#v", payload)
	}
	if body.authority != testBodyAuthorityRaw {
		t.Fatalf("authority changed on read: %s", body.authority)
	}
	if body.IsDirty() {
		t.Fatalf("dirty on read")
	}

	changed, err := mutateTestDocumentBodyJSON(&body, func(payload map[string]any) (bool, error) {
		payload["a"] = float64(2)
		return true, nil
	})
	if err != nil {
		t.Fatalf("MutateJSON() error = %v", err)
	}
	if !changed {
		t.Fatalf("expected changed")
	}
	if body.authority != testBodyAuthorityJSON {
		t.Fatalf("authority not switched: %s", body.authority)
	}
	if !body.IsDirty() {
		t.Fatalf("expected dirty after mutation")
	}

	raw, err := body.Raw()
	if err != nil {
		t.Fatalf("Raw() error = %v", err)
	}
	if string(raw) != `{"a":2}` {
		t.Fatalf("unexpected raw: %s", string(raw))
	}
	if body.IsDirty() {
		t.Fatalf("dirty should be cleared after regeneration")
	}
}
