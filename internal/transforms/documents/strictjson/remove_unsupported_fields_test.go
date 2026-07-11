package strictjson

import (
	"bytes"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
)

func TestRemoveUnsupportedFields_RemovesAndReportsLoss(t *testing.T) {
	doc := carrier.WireDocument{Raw: []byte(`{"model":"x","foo":1,"bar":2}`)}
	supported := map[string]struct{}{"model": {}}

	out, report, err := RemoveUnsupportedFields(doc, supported)
	if err != nil {
		t.Fatalf("RemoveUnsupportedFields error: %v", err)
	}
	if !report.Mutated {
		t.Fatal("expected mutation")
	}
	if len(report.Losses) != 2 {
		t.Fatalf("losses=%d want 2", len(report.Losses))
	}
	if !bytes.Equal(out.Raw, []byte(`{"model":"x"}`)) {
		t.Fatalf("raw=%s", string(out.Raw))
	}
}

func TestRemoveUnsupportedFields_Idempotent(t *testing.T) {
	doc := carrier.WireDocument{Raw: []byte(`{"model":"x","foo":1}`)}
	supported := map[string]struct{}{"model": {}}

	once, _, err := RemoveUnsupportedFields(doc, supported)
	if err != nil {
		t.Fatalf("once: %v", err)
	}
	twice, report2, err := RemoveUnsupportedFields(once, supported)
	if err != nil {
		t.Fatalf("twice: %v", err)
	}
	if !bytes.Equal(once.Raw, twice.Raw) {
		t.Fatalf("not idempotent once=%s twice=%s", string(once.Raw), string(twice.Raw))
	}
	if report2.Mutated {
		t.Fatal("second pass should be no-op")
	}
}
