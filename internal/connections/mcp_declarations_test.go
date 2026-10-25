package connections

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
)

func TestMCPDeclarationsSurvivePersistenceAndCredentialRotation(t *testing.T) {
	endpoint, _ := ParseEndpoint("https://example.com/mcp")
	httpSpec, _ := NewMCPHTTPSpec(endpoint, credentialref.Parse("env:OLD"))
	httpSpec, err := httpSpec.WithOperatorDeclarations("Shared docs account", "Read and write docs")
	if err != nil {
		t.Fatal(err)
	}
	stdio, _ := NewMCPStdioSpec("server", nil, map[string]credentialref.Ref{"TOKEN": credentialref.Parse("env:OLD")})
	stdio, err = stdio.WithOperatorDeclarations("Local account", "Read local files")
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range []Spec{httpSpec, stdio} {
		id, _ := connectionidentity.Parse("cn_declarations")
		value, err := New(id, "Declared", spec, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		record, err := encodeRecord(value)
		if err != nil {
			t.Fatal(err)
		}
		var invalid mcpRecord
		if json.Unmarshal(record.Spec, &invalid) != nil {
			t.Fatal("invalid fixture")
		}
		invalid.ExecutionIdentity = " invalid"
		damaged := record
		damaged.Spec, _ = json.Marshal(invalid)
		if _, err := decodeRecord(damaged, nil); err == nil {
			t.Fatal("invalid persisted declaration admitted")
		}
		restored, err := decodeRecord(record, func(_ string) bool { return true })
		if err != nil {
			t.Fatal(err)
		}
		environment := ""
		if _, ok := spec.(MCPStdioSpec); ok {
			environment = "TOKEN"
		}
		changed, err := restored.ReplaceCredential(restored.Revision(), environment, credentialref.Parse("env:OLD"), credentialref.Parse("env:NEW"), time.Now().Add(time.Second))
		if err != nil {
			t.Fatal(err)
		}
		switch after := changed.Spec().(type) {
		case MCPHTTPSpec:
			if after.ExecutionIdentity() != httpSpec.ExecutionIdentity() || after.PermissionSummary() != httpSpec.PermissionSummary() {
				t.Fatal("HTTP declaration lost")
			}
		case MCPStdioSpec:
			if after.ExecutionIdentity() != stdio.ExecutionIdentity() || after.PermissionSummary() != stdio.PermissionSummary() {
				t.Fatal("stdio declaration lost")
			}
		}
	}
	for _, pair := range [][2]string{{" leading", "valid"}, {"valid", "line\nbreak"}, {strings.Repeat("x", 257), "valid"}, {"valid", strings.Repeat("x", 4097)}} {
		if _, err := httpSpec.WithOperatorDeclarations(pair[0], pair[1]); err == nil {
			t.Fatal("invalid HTTP declaration admitted")
		}
		if _, err := stdio.WithOperatorDeclarations(pair[0], pair[1]); err == nil {
			t.Fatal("invalid stdio declaration admitted")
		}
	}
}
