package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestInventoryIncludesDynamicRegistrationsAndForbiddenNamespaceConsumers(t *testing.T) {
	root := t.TempDir()
	put := func(path, body string) {
		t.Helper()
		path = filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	put("internal/adapters/inbound/httpapi/routes.go", `package httpapi
func routes(){mux.HandleFunc("GET "+prefix+"/workspaces/{workspace}",h.read);mux.Handle("GET /api/v1/workspaces/{workspace}/publications",h)}`)
	put("internal/adapters/inbound/cli/client.go", `package cli
const old="/_swobu/status"`)
	put("internal/adapters/inbound/httpapi/session.go", `package httpapi
func session(){if r.URL.Path=="/api/v1/browser-session" { return }}`)
	put("webui/node_modules/vendor/routes.go", `invalid go /_swobu`)
	put("webui/src/api/transport.ts", `const path='/api/v1/workspaces';`)
	value, err := collect(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(value.Registrations) != 2 {
		t.Fatalf("registrations=%+v", value.Registrations)
	}
	foundClient := false
	for _, reference := range value.NamespaceReferences {
		if reference.Source == "webui/src/api/transport.ts" && reference.Namespace == "/api/v1" {
			foundClient = true
		}
	}
	if !foundClient {
		t.Fatal("canonical client under api directory omitted")
	}
	foundDispatch := false
	for _, path := range value.DispatchPaths {
		if path.Expression == "/api/v1/browser-session" {
			foundDispatch = true
		}
	}
	if !foundDispatch {
		t.Fatal("manual session dispatch omitted")
	}
	owners := map[string]bool{}
	for _, r := range value.Registrations {
		owners[r.Owner] = true
	}
	if !owners["retain"] {
		t.Fatalf("owners=%v", owners)
	}
	found := false
	for _, c := range value.LegacyConsumers {
		if c.Namespace == "/_swobu" && c.Owner == "L01" {
			found = true
		}
	}
	if !found {
		t.Fatal("missing legacy caller")
	}
	// A newly introduced caller must change the measured baseline.
	before := len(value.LegacyConsumers)
	put("internal/adapters/inbound/cli/new.go", `package cli
const forbidden="/_swobu/new"`)
	value, err = collect(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(value.LegacyConsumers) != before+1 {
		t.Fatal("new legacy caller silently omitted")
	}
	beforeDigest := ""
	for _, c := range value.LegacyConsumers {
		if c.Source == "internal/adapters/inbound/cli/new.go" {
			beforeDigest = c.EvidenceSHA256
		}
	}
	put("internal/adapters/inbound/cli/new.go", `package cli
const forbidden="/_swobu/new"
const another="/_swobu/another"`)
	value, err = collect(root)
	if err != nil {
		t.Fatal(err)
	}
	foundChange := false
	for _, c := range value.LegacyConsumers {
		if c.Source == "internal/adapters/inbound/cli/new.go" {
			foundChange = c.Occurrences == 2 && c.EvidenceSHA256 != beforeDigest
		}
	}
	if !foundChange {
		t.Fatal("new legacy call in an existing file silently omitted")
	}

}

// A canonical namespace does not make a retired resource part of the target API.
func TestCanonicalRoutesAreRetained(t *testing.T) {
	for path, want := range map[string]string{
		"/api/v1/workspaces/{workspace}/permissions/{principal}": "G01",
		"/api/v1/principals/{access}/credentials":                "G01",
		"/api/v1/workspaces/{workspace}/model-routes":            "retain",
		"/api/v1/workspaces/{workspace}/publications/relay":      "retain",
		"/api/v1/remote-addresses":                               "retain",
		"/api/v1/client-configurations":                          "E02",
		"/api/v1/agent-sessions/{id}/limits/apply":               "G02",
		"/api/v1/status":      "M01",
		"/api/v1/activity":    "retain",
		"/api/v1/workspaces":  "retain",
		"/api/v1/connections": "retain",
	} {
		if got := routeOwner(path); got != want {
			t.Errorf("%s: owner=%s, want %s", path, got, want)
		}
	}
}

func TestProductInventoryIncludesSiblingConsumers(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "opencore")
	for _, directory := range []string{root, filepath.Join(parent, "verification"), filepath.Join(parent, "tools")} {
		if err := os.MkdirAll(directory, 0755); err != nil {
			t.Fatal(err)
		}
	}
	caller := filepath.Join(parent, "verification", "client.go")
	if err := os.WriteFile(caller, []byte(`package verification
const endpoint="/_swobu/status"`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, "tools", "client.ts"), []byte(`const endpoint="/api/v1/activity";`), 0644); err != nil {
		t.Fatal(err)
	}
	value, err := collectProduct(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(value.LegacyConsumers) != 1 || value.LegacyConsumers[0].Source != "../verification/client.go" || value.LegacyConsumers[0].Owner != "M01" {
		t.Fatalf("legacy callers=%+v", value.LegacyConsumers)
	}
	if len(value.NamespaceReferences) != 2 {
		t.Fatalf("references=%+v", value.NamespaceReferences)
	}
	before := value.LegacyConsumers[0].EvidenceSHA256
	if err := os.WriteFile(caller, []byte(`package verification
const endpoint="/_swobu/status"
const added="/_swobu/down"`), 0644); err != nil {
		t.Fatal(err)
	}
	changed, err := collectProduct(root)
	if err != nil {
		t.Fatal(err)
	}
	if changed.LegacyConsumers[0].EvidenceSHA256 == before || changed.LegacyConsumers[0].Occurrences != 2 {
		t.Fatal("new sibling call did not alter inventory")
	}
	// Public runtime checkouts have no private siblings and retain the same lane.
	isolated := t.TempDir()
	if _, err := collectProduct(isolated); err != nil {
		t.Fatal(err)
	}
}

func TestInventoryDetectsHandlerReplacementWithoutPathChange(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "internal/bootstrap/mux.go")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	capture := func(handler string) string {
		t.Helper()
		body := "package bootstrap\nfunc mount(){mux.Handle(\"/api/v1/workspaces/\",\n " + handler + ",\n)}"
		if err := os.WriteFile(path, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
		value, err := collect(root)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	if capture("workspaceControl") == capture("workspaceData") {
		t.Fatal("handler replacement leaves inventory unchanged")
	}
}

func TestInventoryDetectsHandlerConstructorReplacement(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "internal/bootstrap/mux.go")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	capture := func(constructor string) string {
		t.Helper()
		body := "package bootstrap\nfunc mount(){handler := " + constructor + "(service)\n mux.Handle(\"/api/v1/workspaces/\", handler)}"
		if err := os.WriteFile(path, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
		value, err := collect(root)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	if capture("NewControlHandler") == capture("NewDataHandler") {
		t.Fatal("constructor replacement leaves inventory unchanged")
	}
}
