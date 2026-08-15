package openai

import (
	"reflect"
	"strings"
	"testing"
)

func TestDecodeModelRowsPreservesRowsForAuthoringProjection(t *testing.T) {
	rows, err := DecodeModelRows(strings.NewReader(`{"data":[
		{"id":"model-b","archived":false},
		{"id":"model-a","archived":true},
		{"id":"model-b","archived":true},
		{"id":""}
	]}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{rows[0].ID(), rows[1].ID(), rows[2].ID()}; !reflect.DeepEqual(got, []string{"model-b", "model-a", "model-b"}) {
		t.Fatalf("row IDs = %v", got)
	}
	if got := string(rows[1].RawJSON()); got != `{"id":"model-a","archived":true}` {
		t.Fatalf("raw row = %s", got)
	}
	raw := rows[0].RawJSON()
	raw[0] = 'X'
	if got := string(rows[0].RawJSON()); got != `{"id":"model-b","archived":false}` {
		t.Fatalf("RawJSON was not defensive: %s", got)
	}
}

func TestDecodeModelIDsRetainsSortedUniqueCompatibility(t *testing.T) {
	models, err := DecodeModelIDs(strings.NewReader(`{"data":[{"id":"model-b"},{"id":"model-a"},{"id":"model-b"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(models, []string{"model-a", "model-b"}) {
		t.Fatalf("models = %v", models)
	}
}
