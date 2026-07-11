package contentparts

import (
	"bytes"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
)

func TestPreserveToolOutputParts_RemovesUnsupportedFilePartsWithLoss(t *testing.T) {
	doc := carrier.WireDocument{Raw: []byte(`{
		"input":[
			{"type":"function_call_output","call_id":"c1","output":[
				{"type":"text","text":"ok"},
				{"type":"input_file","file_id":"f1"}
			]}
		]
	}`)}
	out, report, err := PreserveToolOutputParts(doc, false)
	if err != nil {
		t.Fatalf("PreserveToolOutputParts error: %v", err)
	}
	if !report.Mutated {
		t.Fatal("expected mutation")
	}
	if len(report.Losses) == 0 {
		t.Fatal("expected loss for unsupported file part")
	}
	if bytes.Contains(out.Raw, []byte(`"input_file"`)) {
		t.Fatalf("unsupported file part must be removed: %s", string(out.Raw))
	}
	if !bytes.Contains(out.Raw, []byte(`"text"`)) {
		t.Fatalf("text part must remain: %s", string(out.Raw))
	}
}

func TestPreserveToolOutputParts_PreservesFilePartsWhenSupported(t *testing.T) {
	doc := carrier.WireDocument{Raw: []byte(`{
		"input":[
			{"type":"function_call_output","call_id":"c1","output":[
				{"type":"text","text":"ok"},
				{"type":"input_file","file_id":"f1"}
			]}
		]
	}`)}
	out, report, err := PreserveToolOutputParts(doc, true)
	if err != nil {
		t.Fatalf("PreserveToolOutputParts error: %v", err)
	}
	if report.Mutated {
		t.Fatal("expected no mutation")
	}
	if len(report.Losses) != 0 {
		t.Fatalf("losses=%d want 0", len(report.Losses))
	}
	if !bytes.Contains(out.Raw, []byte(`"input_file"`)) {
		t.Fatalf("file part should remain: %s", string(out.Raw))
	}
}

func TestPreserveToolOutputParts_Idempotent(t *testing.T) {
	doc := carrier.WireDocument{Raw: []byte(`{
		"input":[
			{"type":"function_call_output","call_id":"c1","output":[
				{"type":"text","text":"ok"},
				{"type":"input_file","file_id":"f1"}
			]}
		]
	}`)}
	once, _, err := PreserveToolOutputParts(doc, false)
	if err != nil {
		t.Fatalf("once: %v", err)
	}
	twice, report2, err := PreserveToolOutputParts(once, false)
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
