package opaqueturnstate

import (
	"bytes"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
)

type memoryStore struct {
	data map[string]string
}

func (m *memoryStore) Put(key string, value string) {
	if m.data == nil {
		m.data = map[string]string{}
	}
	m.data[key] = value
}

func (m *memoryStore) Get(key string) (string, bool) {
	v, ok := m.data[key]
	return v, ok
}

func TestCaptureOpaqueFields_CapturesThoughtSignature(t *testing.T) {
	store := &memoryStore{}
	doc := carrier.WireDocument{Raw: []byte(`{"output":[{"content":[{"type":"reasoning","thoughtSignature":"sig_1"}]}]}`)}

	report := CaptureOpaqueFields(doc, store)
	if report.Mutated {
		t.Fatal("capture should not mutate carrier")
	}
	if got, ok := store.Get(thoughtStateStoreKey); !ok || got != "sig_1" {
		t.Fatalf("captured=%q ok=%v", got, ok)
	}
}

func TestReplayOpaqueFields_WritesProviderRequestPayload(t *testing.T) {
	store := &memoryStore{data: map[string]string{thoughtStateStoreKey: "sig_2"}}
	doc := carrier.WireDocument{Raw: []byte(`{"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}]}`)}
	once, report, err := ReplayOpaqueFields(doc, store, true)
	if err != nil {
		t.Fatalf("ReplayOpaqueFields error: %v", err)
	}
	if !report.Mutated {
		t.Fatal("expected payload mutation")
	}
	if bytes.Equal(once.Raw, doc.Raw) {
		t.Fatalf("provider payload did not change: before=%s after=%s", string(doc.Raw), string(once.Raw))
	}
	if !bytes.Contains(once.Raw, []byte(`"thoughtSignature":"sig_2"`)) {
		t.Fatalf("provider payload missing replay signature: %s", string(once.Raw))
	}
}

func TestReplayOpaqueFields_Idempotent(t *testing.T) {
	store := &memoryStore{data: map[string]string{thoughtStateStoreKey: "sig_3"}}
	doc := carrier.WireDocument{Raw: []byte(`{"input":[]}`)}
	once, _, err := ReplayOpaqueFields(doc, store, true)
	if err != nil {
		t.Fatalf("once: %v", err)
	}
	twice, report2, err := ReplayOpaqueFields(once, store, true)
	if err != nil {
		t.Fatalf("twice: %v", err)
	}
	if !bytes.Equal(once.Raw, twice.Raw) {
		t.Fatalf("raw changed on second pass: once=%s twice=%s", string(once.Raw), string(twice.Raw))
	}
	if report2.Mutated {
		t.Fatal("second pass should be no-op")
	}
}

func TestOpaqueCaptureReportsInvalidJSON(t *testing.T) {
	store := &memoryStore{}
	doc := carrier.WireDocument{Raw: []byte(`{"output":[`)}
	report := CaptureOpaqueFields(doc, store)
	if len(report.Losses) != 1 {
		t.Fatalf("losses=%#v", report.Losses)
	}
	if report.Losses[0].Reason != "opaque_capture_invalid_json" {
		t.Fatalf("loss reason=%q", report.Losses[0].Reason)
	}
}

func TestReplayOpaqueFields_InvalidJSONReturnsError(t *testing.T) {
	store := &memoryStore{data: map[string]string{thoughtStateStoreKey: "sig_2"}}
	doc := carrier.WireDocument{Raw: []byte(`{"input":[`)}
	_, _, err := ReplayOpaqueFields(doc, store, true)
	if err == nil {
		t.Fatal("expected invalid json error")
	}
}
