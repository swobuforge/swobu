package apischema_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/adapters/inbound/httpapi"
	"github.com/swobuforge/swobu/internal/adapters/inbound/httpapi/apischema"
	connectionapp "github.com/swobuforge/swobu/internal/app/operator/connections"
)

func moduleRoot(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	for dir := filepath.Dir(file); ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		if filepath.Dir(dir) == dir {
			t.Fatal("OpenCore module root missing")
		}
	}
}

func TestDocumentDeterminismAndCommittedDrift(t *testing.T) {
	first, err := apischema.Document()
	if err != nil {
		t.Fatal(err)
	}
	second, err := apischema.Document()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("nondeterministic generation")
	}
	committed, err := os.ReadFile(filepath.Join(moduleRoot(t), "api/openapi.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, committed) {
		t.Fatal("OpenAPI drift: run make openapi-generate")
	}
}

func TestProblemSchemaMatchesWireFieldsAndOperationConventions(t *testing.T) {
	raw, _ := apischema.Document()
	var document struct {
		Paths map[string]map[string]struct {
			ID         string           `json:"operationId"`
			Stage      string           `json:"x-swobu-contract-stage"`
			Parameters []map[string]any `json:"parameters"`
		} `json:"paths"`
		Components struct {
			Schemas map[string]struct {
				Properties map[string]any `json:"properties"`
				Required   []string       `json:"required"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	schema := document.Components.Schemas["ControlProblem"]
	wire := reflect.TypeOf(httpapi.ControlProblem{})
	required := map[string]bool{}
	for _, name := range schema.Required {
		required[name] = true
	}
	if len(schema.Properties) != wire.NumField() {
		t.Fatal("Problem schema field count drift")
	}
	for i := 0; i < wire.NumField(); i++ {
		tag := wire.Field(i).Tag.Get("json")
		parts := strings.Split(tag, ",")
		if _, ok := schema.Properties[parts[0]]; !ok {
			t.Errorf("missing wire field %s", parts[0])
		}
		if required[parts[0]] != (len(parts) == 1) {
			t.Errorf("required mismatch %s", tag)
		}
	}
	ids := map[string]bool{}
	for path, item := range document.Paths {
		if strings.Contains(path, "/_swobu") || strings.HasPrefix(path, "/workspaces/") {
			t.Errorf("legacy schema path %s", path)
		}
		for method, op := range item {
			if ids[op.ID] || op.ID == "" {
				t.Errorf("duplicate/missing operation ID %s", op.ID)
			}
			ids[op.ID] = true
			if op.Stage != "implemented" {
				t.Errorf("unqualified operation in public client contract: %s %s (%s)", method, path, op.Stage)
			}
			if method == "put" || method == "patch" || method == "delete" {
				found := false
				for _, p := range op.Parameters {
					found = found || p["$ref"] == "#/components/parameters/IfMatch" || (p["name"] == "If-Match" && p["in"] == "header")
				}
				if !found {
					t.Errorf("missing revision header %s %s", method, path)
				}
			}
		}
	}
}

func TestConnectionReadSchemasMatchPublicWireFields(t *testing.T) {
	raw, err := apischema.Document()
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]any `json:"properties"`
				Required   []string       `json:"required"`
				OneOf      []struct {
					Properties map[string]any `json:"properties"`
					Required   []string       `json:"required"`
				} `json:"oneOf"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		wire any
	}{
		{"ConnectionReference", connectionapp.Reference{}}, {"ConnectionReferences", connectionapp.References{}},
		{"ConnectionCheck", connectionapp.Check{}}, {"ConnectionStandardModel", connectionapp.StandardModel{}},
		{"ConnectionZAIModel", connectionapp.ZAIModel{}}, {"ConnectionBedrockModel", connectionapp.BedrockModel{}},
		{"ConnectionCustomModel", connectionapp.CustomModel{}}, {"ConnectionMCPHTTP", connectionapp.MCPHTTP{}},
		{"ConnectionMCPStdio", connectionapp.MCPStdio{}}, {"ConnectionSearch", connectionapp.Search{}},
		{"Connection", connectionapp.Connection{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			schema, exists := document.Components.Schemas[test.name]
			if !exists {
				t.Fatal("missing schema")
			}
			properties, required := schema.Properties, schema.Required
			variants := len(schema.OneOf)
			if test.name == "Connection" && variants != 3 {
				t.Fatal("missing kind variants")
			}
			if variants == 0 {
				variants = 1
			}
			for variant := 0; variant < variants; variant++ {
				if len(schema.OneOf) != 0 {
					properties, required = schema.OneOf[variant].Properties, schema.OneOf[variant].Required
				}
				wire := reflect.TypeOf(test.wire)
				if len(properties) != wire.NumField() {
					t.Fatalf("field count drift: %s", test.name)
				}
				for i := 0; i < wire.NumField(); i++ {
					tag := strings.Split(wire.Field(i).Tag.Get("json"), ",")
					if _, ok := properties[tag[0]]; !ok {
						t.Fatalf("missing field %s", tag[0])
					}
					isRequired := false
					for _, name := range required {
						isRequired = isRequired || name == tag[0]
					}
					if isRequired != (len(tag) == 1) {
						t.Fatalf("required drift: %s", tag[0])
					}
				}
			}
		})
	}
}
