package responsesnative

import (
	"fmt"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestItemsPreservesRawItemsAndClonesDefensively(t *testing.T) {
	source := []byte(`{"type":"future_item","large":9007199254740993,"nullable":null}`)
	batch, err := NewItems([][]byte{source})
	if err != nil {
		t.Fatal(err)
	}
	source[2] = 'X'
	got := batch.JSONObjects()
	want := `{"type":"future_item","large":9007199254740993,"nullable":null}`
	if string(got[0]) != want {
		t.Fatalf("raw item changed: %s", got[0])
	}
	got[0][2] = 'Y'
	if string(batch.JSONObjects()[0]) != want {
		t.Fatal("wire accessor leaked mutable storage")
	}
	if rendered := fmt.Sprintf("%#v", batch); strings.Contains(rendered, "future_item") || !strings.Contains(rendered, "REDACTED") {
		t.Fatalf("opaque state leaked through formatting: %s", rendered)
	}
}

func TestItemsRejectsIncompleteOrNonObjectJSON(t *testing.T) {
	for _, raw := range []string{"", `[]`, `{"type":`} {
		if _, err := NewItems([][]byte{[]byte(raw)}); err == nil {
			t.Fatalf("expected %q to fail", raw)
		}
	}
}

func TestItemsDistinguishesAbsentFromPresentEmpty(t *testing.T) {
	if !(Items{}).IsZero() {
		t.Fatal("zero Items must mean absent")
	}

	empty, err := NewItems(nil)
	if err != nil {
		t.Fatal(err)
	}
	if empty.IsZero() || empty.Len() != 0 {
		t.Fatal("validated empty collection must mean present but empty")
	}
}

func TestRequestStateClonesTranscriptWithoutOwningCanonicalSemantics(t *testing.T) {
	input, err := NewItems([][]byte{[]byte(`{"type":"message","content":"one"}`)})
	if err != nil {
		t.Fatal(err)
	}
	output, err := NewItems([][]byte{[]byte(`{"type":"message","content":"two"}`)})
	if err != nil {
		t.Fatal(err)
	}
	turn := NewTurn(canonical.CanonicalRequest{}, input, output)
	state := NewRequestState(input, NewHistory([]Turn{turn}))

	inputCopy := state.Input().JSONObjects()
	inputCopy[0][0] = 'X'
	turnCopy := state.History().Turns()
	outputCopy := turnCopy[0].Output().JSONObjects()
	outputCopy[0][0] = 'Y'

	if got := string(state.Input().JSONObjects()[0]); got != `{"type":"message","content":"one"}` {
		t.Fatalf("request input mutated through clone: %s", got)
	}
	if got := string(state.History().Turns()[0].Output().JSONObjects()[0]); got != `{"type":"message","content":"two"}` {
		t.Fatalf("history output mutated through clone: %s", got)
	}
}
