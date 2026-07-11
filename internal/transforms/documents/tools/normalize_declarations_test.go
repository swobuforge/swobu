package tools

import (
	"bytes"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
)

func TestNormalizeDeclarations_RewritesAndReportsLoss(t *testing.T) {
	doc := carrier.WireDocument{Raw: []byte(`{"tools":[{"type":"function","function":{"name":"ok"}},{"type":"namespace","name":"ns"},{"type":"hosted"}]}`)}
	out, report, err := NormalizeDeclarations(doc)
	if err != nil {
		t.Fatalf("NormalizeDeclarations error: %v", err)
	}
	if !report.Mutated {
		t.Fatal("expected mutation")
	}
	if len(report.Losses) == 0 {
		t.Fatal("expected at least one loss for unsupported hosted tool")
	}
	if !bytes.Contains(out.Raw, []byte(`"type":"function"`)) {
		t.Fatalf("normalized payload missing function tools: %s", string(out.Raw))
	}
	if bytes.Contains(out.Raw, []byte(`"type":"hosted"`)) {
		t.Fatalf("unsupported hosted tool must be removed: %s", string(out.Raw))
	}
}

func TestNormalizeDeclarations_Idempotent(t *testing.T) {
	doc := carrier.WireDocument{Raw: []byte(`{"tools":[{"type":"namespace","name":"ns"}]}`)}
	once, _, err := NormalizeDeclarations(doc)
	if err != nil {
		t.Fatalf("once: %v", err)
	}
	if bytes.Contains(once.Raw, []byte(`"namespace"`)) {
		t.Fatalf("namespace tool without function must be removed: %s", string(once.Raw))
	}
	twice, report2, err := NormalizeDeclarations(once)
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
