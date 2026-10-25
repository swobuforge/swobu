package configstore

import (
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/routing"
)

func TestReferenceCodecRoundTripAndStrictAdmission(t *testing.T) {
	const source = `schema_version: 5
workspaces:
  dev:
    routes:
      - pattern: main
        tiers:
          - targets:
              - id: shared
                version: 7
                model: synthetic
                protocol: responses
                connection_id: cn_account
    mcp_mounts: {}
`
	config, err := ValidateDocument([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	target := config.Workspaces()[0].Routes()[0].Tiers()[0].Targets()[0]
	if target.ConnectionID().String() != "cn_account" || target.Version() != 7 {
		t.Fatal("reference identity lost")
	}
	encoded, err := encodeDocument(config, routing.ReferenceSchemaVersion)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "connection:") || !strings.Contains(string(encoded), "connection_id: cn_account") {
		t.Fatal("embedded target encoded")
	}
	reopened, err := ValidateDocument(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.Workspaces()[0].Routes()[0].Tiers()[0].Targets()[0]; got != target {
		t.Fatal("target changed on reopen")
	}
	for _, tc := range []struct{ name, old, replacement string }{
		{"old schema", "schema_version: 5", "schema_version: 4"},
		{"embedded field", "connection_id: cn_account", "connection: {}"},
		{"missing ID", "connection_id: cn_account", "connection_id: ''"},
		{"quoted version", "version: 7", "version: '7'"},
		{"hex version", "version: 7", "version: 0x7"},
		{"zero version", "version: 7", "version: 0"},
		{"omitted version", "                version: 7\n", ""},
		{"missing protocol", "protocol: responses", "protocol: ''"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ValidateDocument([]byte(strings.Replace(source, tc.old, tc.replacement, 1))); err == nil {
				t.Fatal("invalid current document admitted")
			}
		})
	}
}
