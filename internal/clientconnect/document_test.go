package clientconnect

import (
	"bytes"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2/unstable"
)

func TestJSONEditorConformance(t *testing.T) {
	for _, tc := range []struct {
		name   string
		editor jsonEditor
		raw    []byte
	}{
		{name: "strict", editor: jsonEditor{}, raw: []byte("{\n  \"large\": 9007199254740993123456789,\n  \"env\": {\"KEEP\": true, \"URL\": \"old\"}\n}\n")},
		{name: "jsonc", editor: jsonEditor{allowComments: true}, raw: []byte("{\n  // keep\n  \"env\": {\"KEEP\": true, \"URL\": \"old\",},\n}\n")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := keyPath{"env", "URL"}
			got, exists, err := tc.editor.String(tc.raw, path)
			if err != nil || !exists || got != "old" {
				t.Fatalf("String = %q/%v/%v", got, exists, err)
			}
			next, err := tc.editor.SetString(tc.raw, path, `new\"value`)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(bytes.Replace(tc.raw, []byte(`"old"`), []byte(`"new\\\"value"`), 1), next) {
				t.Fatalf("unrelated source changed:\n%s", next)
			}
		})
	}
}

func TestJSONEditorAddsObjectsAndRejectsAmbiguityOrWrongShape(t *testing.T) {
	editor := jsonEditor{}
	next, err := editor.SetString([]byte("{}\n"), keyPath{"providers", "openai", "baseUrl"}, "http://swobu")
	if err != nil {
		t.Fatal(err)
	}
	if got, exists, err := editor.String(next, keyPath{"providers", "openai", "baseUrl"}); err != nil || !exists || got != "http://swobu" {
		t.Fatalf("String = %q/%v/%v; %s", got, exists, err, next)
	}
	for _, raw := range [][]byte{[]byte(`{"env":{"URL":"a","URL":"b"}}`), []byte(`{"env":[]}`)} {
		if _, err := editor.SetString(raw, keyPath{"env", "URL"}, "new"); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	if _, err := editor.SetString([]byte("{// comment\n}\n"), keyPath{"URL"}, "new"); err == nil {
		t.Fatal("strict editor accepted JSONC")
	}
}

func TestTOMLEditorConformance(t *testing.T) {
	editor := tomlEditor{}
	path := keyPath{"openai_base_url"}
	original := []byte("# keep\nopenai_base_url = 'https://old' # owned\n\n[profiles.work]\nmodel = \"keep\"\n")
	got, exists, err := editor.String(original, path)
	if err != nil || !exists || got != "https://old" {
		t.Fatalf("String = %q/%v/%v", got, exists, err)
	}
	next, err := editor.SetString(original, path, "http://swobu")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(string(original), "'https://old'", `"http://swobu"`, 1)
	if string(next) != want {
		t.Fatalf("source changed:\n%s", next)
	}
	inserted, err := editor.SetString([]byte("model = \"keep\"\n\n[profile.x]\nmodel = \"x\"\n"), path, "http://swobu")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(inserted), "model = \"keep\"\n\nopenai_base_url = \"http://swobu\"\n[profile.x]") {
		t.Fatalf("inserted:\n%s", inserted)
	}
}

func TestTOMLEditorAddsAndUpdatesNestedTableStrings(t *testing.T) {
	editor := tomlEditor{}
	original := []byte("# keep\n[provider]\nname = \"keep\"\n[other]\nx = 1\n")
	next, err := editor.SetString(original, keyPath{"provider", "url"}, "new")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(next), "[provider]\nname = \"keep\"\nurl = \"new\"\n[other]") {
		t.Fatalf("nested insertion:\n%s", next)
	}
	next, err = editor.SetString(next, keyPath{"provider", "url"}, "newer")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(next), "[provider]") != 1 || !strings.Contains(string(next), `url = "newer"`) {
		t.Fatalf("nested replacement:\n%s", next)
	}
}

func TestTOMLEditorInsertsBeforeCompleteArrayTableExpression(t *testing.T) {
	editor := tomlEditor{}
	original := []byte("[[products]]\nname = \"x\"\n")
	next, err := editor.SetString(original, keyPath{"openai_base_url"}, "http://swobu")
	if err != nil {
		t.Fatal(err)
	}
	want := "openai_base_url = \"http://swobu\"\n" + string(original)
	if string(next) != want {
		t.Fatalf("array table insertion corrupted source:\n got: %s\nwant: %s", next, want)
	}
	parser := unstable.Parser{}
	parser.Reset(next)
	for parser.NextExpression() {
	}
	if err := parser.Error(); err != nil {
		t.Fatalf("result is invalid TOML: %v\n%s", err, next)
	}
}
